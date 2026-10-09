package dotgit

import (
	"crypto"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
	githash "github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

var _ storer.PackReader = (*PackWriter)(nil)

var errReadPackCalled = errors.New("dotgit: ReadPack already called")

// A PackWriter writes a packfile to a temporary file, building its index as
// the data arrives through [PackWriter.Write] or [PackWriter.ReadPack].
// [PackWriter.Close] moves the pack and its index into place.
type PackWriter struct {
	Notify func(plumbing.Hash, *idxfile.Writer)

	fs       billy.Filesystem
	fr, fw   billy.File
	synced   *syncedReader
	checksum plumbing.Hash
	size     int64
	parser   *packfile.Parser
	writer   *idxfile.Writer
	parsed   chan struct{} // closed when buildIndex returns
	parseErr error         // valid once parsed is closed
	readPack atomic.Bool   // set once ReadPack is called
	format   formatcfg.ObjectFormat
	writeRev bool
	// promisor, when non-nil, writes a .promisor sidecar next to the pack
	// carrying these contents. A nil value leaves the pack unmarked.
	promisor *string
}

func newPackWrite(fs billy.Filesystem, format formatcfg.ObjectFormat, writeRev bool) (*PackWriter, error) {
	fw, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_pack_")
	if err != nil {
		return nil, err
	}

	fr, err := fs.Open(fw.Name())
	if err != nil {
		return nil, err
	}

	writer := &PackWriter{
		fs:       fs,
		fw:       fw,
		fr:       fr,
		synced:   newSyncedReader(fw, fr),
		parsed:   make(chan struct{}),
		format:   format,
		writeRev: writeRev,
	}

	writer.checksum.ResetBySize(format.Size())

	go writer.buildIndex()
	return writer, nil
}

func (w *PackWriter) buildIndex() {
	defer close(w.parsed)
	w.writer = new(idxfile.Writer)

	w.parser = packfile.NewParser(w.synced,
		packfile.WithScannerObservers(w.writer),
		packfile.WithObjectFormat(w.format))

	h, err := w.parser.Parse()
	if err != nil {
		w.parseErr = err
		return
	}

	w.checksum = h
	w.size = w.parser.Size()
}

// waitBuildIndex waits for buildIndex and returns its error, treating an
// empty pack as success. It may be called any number of times.
func (w *PackWriter) waitBuildIndex() error {
	<-w.parsed
	if errors.Is(w.parseErr, packfile.ErrEmptyPackfile) {
		return nil
	}

	return w.parseErr
}

// Write appends p to the pack. Once [PackWriter.ReadPack] has been called,
// Write returns an error.
func (w *PackWriter) Write(p []byte) (int, error) {
	if w.readPack.Load() {
		return 0, errReadPackCalled
	}

	return w.synced.Write(p)
}

// ReadPack reads a packfile from r and writes it as [PackWriter.Write] does,
// returning once the pack trailer has been parsed rather than when r reaches
// EOF. Data previously passed to Write is parsed first. ReadPack calls Read
// on r only when the parser needs more data, so it reads past the trailer
// no more than the final Read returns. Those bytes are counted in the result
// but are not part of the saved pack.
//
// ReadPack returns the number of bytes read from r and any error parsing the
// pack, which wraps an error r returned before the trailer. An error returned
// by the Read that completes the pack is ignored. If r is empty, ReadPack
// returns 0, nil.
//
// ReadPack returns an error if it has already been called. Write must not be
// called concurrently with it, and [PackWriter.Close] must still be called.
func (w *PackWriter) ReadPack(r io.Reader) (int64, error) {
	if !w.readPack.CompareAndSwap(false, true) {
		return 0, errReadPackCalled
	}

	cr := &countingReader{r: r}
	w.synced.pull(cr)
	err := w.waitBuildIndex()
	return cr.n, err
}

// Close closes the temporary file and saves the packfile and its index. If
// nothing was written, Close removes the temporary file and saves nothing.
// If the pack cannot be parsed or saved, Close removes the temporary file,
// leaves no index without its pack, and returns the error.
func (w *PackWriter) Close() (err error) {
	defer func() {
		if err == nil && w.Notify != nil && w.writer != nil && w.writer.Finished() {
			w.Notify(w.checksum, w.writer)
		}
	}()

	if err := w.synced.Close(); err != nil {
		return err
	}

	parseErr := w.waitBuildIndex()

	// Drop anything written after the pack's checksum: it is not part of the
	// pack, and git rejects a .pack with "junk at the end".
	var truncErr error
	if parseErr == nil && w.size > 0 && w.synced.size() > w.size {
		truncErr = w.fw.Truncate(w.size)
	}

	closeErr := errors.Join(truncErr, w.fr.Close(), w.fw.Close())
	if parseErr != nil || closeErr != nil {
		if cleanErr := errors.Join(closeErr, w.clean()); cleanErr != nil {
			return errors.Join(parseErr, cleanErr)
		}
		return parseErr
	}

	if w.writer == nil || !w.writer.Finished() {
		return w.clean()
	}

	return w.save()
}

func (w *PackWriter) clean() error {
	return w.fs.Remove(w.fw.Name())
}

// discard removes paths, newest first, and then the temporary pack. A path
// that is already gone is not an error.
func (w *PackWriter) discard(paths []string) error {
	var errs []error
	for _, p := range slices.Backward(append(paths, w.fw.Name())) {
		if err := w.fs.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// save writes the pack's index files and moves the pack into place next to
// them. On failure it removes the temporary pack and the files it created,
// unless an identical pack is already in place, so that no index is left
// without its pack.
func (w *PackWriter) save() (err error) {
	var (
		created    []string
		packExists bool
	)
	defer func() {
		// Files written next to an identical pack already in place
		// belong to it; only removing the temporary pack failed.
		if err != nil && !packExists {
			err = errors.Join(err, w.discard(created))
		}
	}()

	base := w.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s", w.checksum))

	h := githash.New(crypto.SHA1)
	if w.checksum.Size() == crypto.SHA256.Size() {
		h = githash.New(crypto.SHA256)
	}

	// Pack files are content addressable. Each file is checked
	// individually — if it already exists on disk, skip creating it.
	idxPath := fmt.Sprintf("%s.idx", base)
	exists, err := fileExists(w.fs, idxPath)
	if err != nil {
		return err
	}
	if !exists {
		idx, err := w.fs.Create(idxPath)
		if err != nil {
			return err
		}
		created = append(created, idxPath)

		if err := w.encodeIdx(idx, h); err != nil {
			_ = idx.Close()
			return err
		}

		if err := idx.Close(); err != nil {
			return err
		}
		fixPermissions(w.fs, idxPath)
	}

	if w.writeRev {
		revPath := fmt.Sprintf("%s.rev", base)
		exists, err := fileExists(w.fs, revPath)
		if err != nil {
			return err
		}
		if !exists {
			rev, err := w.fs.Create(revPath)
			if err != nil {
				return err
			}
			created = append(created, revPath)

			if err := w.encodeRev(rev, h); err != nil {
				_ = rev.Close()
				return err
			}

			if err := rev.Close(); err != nil {
				return err
			}
			fixPermissions(w.fs, revPath)
		}
	}

	packPath := fmt.Sprintf("%s.pack", base)
	packExists, err = fileExists(w.fs, packPath)
	if err != nil {
		return err
	}

	// The marker is written before the pack is moved into place, and only for a
	// pack this writer is placing. A pack visible without its marker looks
	// ordinary, so whatever the promisor remote withheld would read as
	// corruption until the marker landed; crashing in that window must not be
	// able to produce a repository git refuses to gc.
	//
	// An identical pack already on disk is left exactly as it is, marked or
	// not. Packs are content addressed, so the same hash means the same
	// objects, and nothing is missing that was not missing before; marking it
	// now would newly declare the repository a partial clone on the strength of
	// a duplicate.
	if w.promisor != nil && !packExists {
		promisorPath := fmt.Sprintf("%s%s", base, promisorExt)
		promisorExists, err := fileExists(w.fs, promisorPath)
		if err != nil {
			return err
		}
		if !promisorExists {
			f, err := w.fs.Create(promisorPath)
			if err != nil {
				return err
			}
			created = append(created, promisorPath)

			if _, err := io.WriteString(f, *w.promisor); err != nil {
				_ = f.Close()
				return err
			}

			if err := f.Close(); err != nil {
				return err
			}
		}
	}

	if !packExists {
		if err := w.fs.Rename(w.fw.Name(), packPath); err != nil {
			return err
		}
		fixPermissions(w.fs, packPath)
	} else {
		// Pack already exists, clean up the temp file.
		return w.clean()
	}

	return nil
}

// fileExists checks whether path already exists as a regular file.
// It returns (true, nil) for an existing regular file, (false, nil) when the
// path does not exist, and (false, err) if the path exists but is not a
// regular file (e.g. a directory or symlink).
func fileExists(fs billy.Filesystem, path string) (bool, error) {
	fi, err := fs.Lstat(path)
	if err != nil {
		return false, nil
	}
	if !fi.Mode().IsRegular() {
		return false, fmt.Errorf("unexpected file type for %q: %s", path, fi.Mode().Type())
	}
	return true, nil
}

func (w *PackWriter) encodeIdx(writer io.Writer, h hash.Hash) error {
	idx, err := w.writer.Index()
	if err != nil {
		return err
	}

	return idxfile.Encode(writer, h, idx)
}

func (w *PackWriter) encodeRev(writer io.Writer, h hash.Hash) error {
	idx, err := w.writer.Index()
	if err != nil {
		return err
	}

	return revfile.Encode(writer, h, idx)
}

// syncedReader lets a reader trail a writer on the same file: Read
// blocks while it has consumed everything written so far, until the
// next Write or Close, or, once pull has set a source, fetches more from
// it. State is guarded by a single mutex so a wake-up can never slip
// between the reader's check and its wait.
type syncedReader struct {
	w io.Writer
	r io.ReadSeeker

	mu            sync.Mutex
	cond          *sync.Cond
	written, read int64
	done          bool
	src           io.Reader // set by pull; read when the reader catches up
	srcBuf        []byte    // what src is read into; set by pull
	srcErr        error     // sticky error from src, io.EOF included
}

// srcBufSize is the size of the buffer src is read into. Read is called
// with the parser's much smaller buffer, and reading src into that would
// take a write to the file for every few KiB. It fits the largest
// side-band-64k packet.
const srcBufSize = 64 << 10

func newSyncedReader(w io.Writer, r io.ReadSeeker) *syncedReader {
	s := &syncedReader{w: w, r: r}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *syncedReader) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)

	s.mu.Lock()
	s.written += int64(n)
	s.mu.Unlock()
	s.cond.Broadcast()

	return n, err
}

// pull makes Read fetch more data from src, appending it to the file, when
// it has consumed everything written so far.
func (s *syncedReader) pull(src io.Reader) {
	s.mu.Lock()
	s.src = src
	s.srcBuf = make([]byte, srcBufSize)
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *syncedReader) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		for s.read >= s.written && s.src == nil && !s.done {
			s.cond.Wait()
		}
		done := s.done
		src, srcBuf, srcErr := s.src, s.srcBuf, s.srcErr
		caughtUp := s.read >= s.written
		s.mu.Unlock()

		if caughtUp && src != nil {
			if srcErr != nil {
				return 0, srcErr
			}

			// One read from the source per call: a git:// client sends
			// nothing after the pack, so a read past its trailer would
			// never return.
			n, err := src.Read(srcBuf)
			if n > 0 {
				if _, werr := s.Write(srcBuf[:n]); werr != nil {
					return 0, werr
				}
			}
			if err != nil {
				s.mu.Lock()
				s.srcErr = err
				s.mu.Unlock()
			}
			if n == 0 {
				return 0, err
			}
		}

		n, err := s.r.Read(p)

		s.mu.Lock()
		s.read += int64(n)
		s.mu.Unlock()

		if err == io.EOF && !done && n == 0 {
			continue
		}

		return n, err
	}
}

func (s *syncedReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		return s.r.Seek(offset, whence)
	}

	p, err := s.r.Seek(offset, whence)

	s.mu.Lock()
	s.read = p
	s.mu.Unlock()

	return p, err
}

func (s *syncedReader) Close() error {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	s.cond.Broadcast()

	return nil
}

// size returns the number of bytes written.
func (s *syncedReader) size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.written
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ObjectWriter writes a single git object to the filesystem.
type ObjectWriter struct {
	objfile.Writer
	fs billy.Filesystem
	f  billy.File
}

func newObjectWriter(fs billy.Filesystem, objectFormat formatcfg.ObjectFormat) (*ObjectWriter, error) {
	f, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_obj_")
	if err != nil {
		return nil, err
	}

	return &ObjectWriter{
		Writer: (*objfile.NewWriter(f, objectFormat)),
		fs:     fs,
		f:      f,
	}, nil
}

// Close finalizes the object and moves it to its permanent location.
func (w *ObjectWriter) Close() error {
	if err := w.Writer.Close(); err != nil {
		return err
	}

	if err := w.f.Close(); err != nil {
		return err
	}

	return w.save()
}

func (w *ObjectWriter) save() error {
	h := w.Hash()
	hex := h.String()
	file := w.fs.Join(objectsPath, hex[0:2], hex[2:h.HexSize()])

	// Loose objects are content addressable, if they already exist
	// we can safely delete the temporary file and short-circuit the
	// operation.
	if _, err := w.fs.Lstat(file); err == nil {
		return w.fs.Remove(w.f.Name())
	}

	if err := w.fs.Rename(w.f.Name(), file); err != nil {
		return err
	}
	fixPermissions(w.fs, file)

	return nil
}
