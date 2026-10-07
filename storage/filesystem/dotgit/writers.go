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

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
	githash "github.com/go-git/go-git/v6/plumbing/hash"
)

// PackWriter is a io.Writer that generates the packfile index simultaneously,
// a packfile.Decoder is used with a file reader to read the file being written
// this operation is synchronized with the write operations.
// The packfile is written in a temp file, when Close is called this file
// is renamed/moved (depends on the Filesystem implementation) to the final
// location, if the PackWriter is not used, nothing is written.
type PackWriter struct {
	Notify func(plumbing.Hash, *idxfile.Writer)

	fs       billy.Filesystem
	fr, fw   billy.File
	synced   *syncedReader
	checksum plumbing.Hash
	size     int64
	parser   *packfile.Parser
	writer   *idxfile.Writer
	result   chan error
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
		result:   make(chan error),
		format:   format,
		writeRev: writeRev,
	}

	writer.checksum.ResetBySize(format.Size())

	go writer.buildIndex()
	return writer, nil
}

func (w *PackWriter) buildIndex() {
	w.writer = new(idxfile.Writer)
	var err error

	w.parser = packfile.NewParser(w.synced,
		packfile.WithScannerObservers(w.writer),
		packfile.WithObjectFormat(w.format))

	h, err := w.parser.Parse()
	if err != nil {
		w.result <- err
		return
	}

	w.checksum = h
	w.size = w.parser.Size()
	w.result <- nil
}

// waitBuildIndex waits until buildIndex function finishes, this can terminate
// with a packfile.ErrEmptyPackfile, this means that nothing was written so we
// ignore the error
func (w *PackWriter) waitBuildIndex() error {
	err := <-w.result
	if errors.Is(err, packfile.ErrEmptyPackfile) {
		return nil
	}

	return err
}

func (w *PackWriter) Write(p []byte) (int, error) {
	return w.synced.Write(p)
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

		close(w.result)
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
// next Write or Close. State is guarded by a single mutex so a wake-up
// can never slip between the reader's check and its wait.
type syncedReader struct {
	w io.Writer
	r io.ReadSeeker

	mu            sync.Mutex
	cond          *sync.Cond
	written, read int64
	done          bool
}

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

func (s *syncedReader) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		for s.read >= s.written && !s.done {
			s.cond.Wait()
		}
		done := s.done
		s.mu.Unlock()

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
