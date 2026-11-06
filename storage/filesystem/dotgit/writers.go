package dotgit

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sync/atomic"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
	githash "github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
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

// Close closes all the file descriptors and save the final packfile, if nothing
// was written, the tempfiles are deleted without writing a packfile.
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
	if parseErr == nil && w.size > 0 && int64(w.synced.written.Load()) > w.size {
		truncErr = w.fw.Truncate(w.size)
	}

	// Close both handles whatever happened, and remove the temporary pack
	// when anything failed, returning the cleanup's errors with the cause.
	closeErr := errors.Join(truncErr, w.fr.Close(), w.fw.Close())
	if parseErr != nil || closeErr != nil {
		return errors.Join(parseErr, closeErr, w.clean())
	}

	if w.writer == nil || !w.writer.Finished() {
		return w.clean()
	}

	return w.save()
}

func (w *PackWriter) clean() error {
	return w.fs.Remove(w.fw.Name())
}

// save moves the pack into place with the files that describe it. Each is
// written to a temporary file and renamed to its final name in the order
// Git's index-pack uses[1]: the promisor marker, the pack, its reverse index
// and the index last, so a reader never finds an index whose pack is missing.
// index-pack creates the marker in place instead; renaming it too means a
// failed save never leaves a partial one. A final name that already exists
// is kept when it holds what this writer would write, as Git checks[2];
// one that does not, such as a truncated index a failed save left, fails
// the save before any file is placed. The promisor marker is kept whatever
// it holds, as index-pack keeps an existing one.
//
// On failure only the temporary files this writer made are removed. A file
// under a final name may be one another writer of the same pack found in
// place and is relying on, so none is ever removed here.
//
// [1]: https://github.com/git/git/blob/v2.54.0/builtin/index-pack.c#L1606-L1638
// [2]: https://github.com/git/git/blob/v2.54.0/object-file.c#L589-L640
func (w *PackWriter) save() (err error) {
	base := w.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s", w.checksum))

	h := githash.New(crypto.SHA1)
	if w.checksum.Size() == crypto.SHA256.Size() {
		h = githash.New(crypto.SHA256)
	}

	temps := map[string]struct{}{w.fw.Name(): {}}
	defer func() {
		if err == nil {
			return
		}
		for name := range temps {
			if rerr := w.fs.Remove(name); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				err = errors.Join(err, rerr)
			}
		}
	}()

	// discard removes temp when final is already in place. As in Git's
	// finalize_object_file[1], failing to remove it only warrants a warning:
	// final is what readers use, and the save carries on.
	//
	// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L652-L712
	discard := func(temp string) {
		if err := w.fs.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
			trace.General.Printf("failed to remove %s: %v", temp, err)
			return
		}
		delete(temps, temp)
	}

	// checked holds the finals already found to match their temporary file.
	checked := map[string]bool{}

	// differ reports that final, already in place, does not hold what temp
	// holds. It names final so that a user can remove one an earlier failed
	// save left, which fails every save of the pack until removed.
	differ := func(temp, final string) error {
		return fmt.Errorf("files %q and %q differ in contents; remove %q if an earlier failed save left it",
			temp, final, final)
	}

	// keep discards temp in favour of final, which is already there. As
	// Git's finalize_object_file checks for a collision[1], final must hold
	// what temp holds when compare is set: a final file whose contents
	// differ, such as a truncated index left by a failed save, fails the
	// save rather than being kept. A final that is gone by the time it is
	// compared is reported as errVanished.
	//
	// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L589-L640
	keep := func(temp, final string, compare bool) error {
		if compare {
			same, err := sameContents(w.fs, temp, final)
			if err != nil {
				return err
			}
			if !same {
				return differ(temp, final)
			}
		}
		discard(temp)
		return nil
	}

	// place renames temp to final and records that it is no longer this
	// writer's to remove, or keeps final when it is already there,
	// including when another writer placed it between the check and the
	// rename. On Windows a rename over a read-only file fails, and placed
	// files are made read-only, so that shows up as a failed rename. A
	// placed file is made read-only unless writable is set. writable also
	// marks a final that is kept whatever it holds, as the promisor marker
	// is; any other final is compared unless the check of every final
	// below already found this one to match. beforeRename, when set, runs
	// once, just before temp is renamed.
	//
	// A final that goes away before it is compared, as a repack removes a
	// redundant pack, is placed anew. Git retries that up to six times[1],
	// and so does place. Only a final place has seen gone is compared
	// again: whatever is there afterwards is another file. A final swapped
	// for another in a single step after the check is still taken as
	// matching, which needs local write access to objects/pack.
	//
	// [1]: https://github.com/git/git/blob/v2.54.0/object-file.c#L700-L705
	place := func(temp, final string, writable bool, beforeRename func() error) error {
		if temp == "" {
			return nil
		}
		trusted := checked[final]
		for retries := 0; ; retries++ {
			exists, err := fileExists(w.fs, final)
			if err != nil {
				return err
			}
			if !exists {
				trusted = false
				if beforeRename != nil {
					if err := beforeRename(); err != nil {
						return err
					}
					beforeRename = nil
				}
				err := w.fs.Rename(temp, final)
				if err == nil {
					delete(temps, temp)
					if !writable {
						fixPermissions(w.fs, final)
					}
					return nil
				}
				if exists, _ := fileExists(w.fs, final); !exists {
					return err
				}
			}
			err = keep(temp, final, !writable && !trusted)
			if !errors.Is(err, errVanished) {
				return err
			}
			if retries >= maxVanishedRetries {
				return fmt.Errorf("unable to write repeatedly vanishing file %q", final)
			}
		}
	}

	// stage writes a temporary file for final, even when final exists, so
	// place can compare the two.
	stage := func(prefix string, write func(io.Writer) error) (string, error) {
		f, err := w.fs.TempFile(w.fs.Join(objectsPath, packPath), prefix)
		if err != nil {
			return "", err
		}
		temps[f.Name()] = struct{}{}
		if err := write(f); err != nil {
			_ = f.Close()
			return "", err
		}
		return f.Name(), f.Close()
	}

	idxPath := base + ".idx"
	idxTemp, err := stage("tmp_idx_", func(f io.Writer) error { return w.encodeIdx(f, h) })
	if err != nil {
		return err
	}

	var revTemp string
	revPath := base + ".rev"
	if w.writeRev {
		revTemp, err = stage("tmp_rev_", func(f io.Writer) error { return w.encodeRev(f, h) })
		if err != nil {
			return err
		}
	}

	packPath := base + ".pack"

	// Every final already in place is checked before any file is placed.
	// Git places the pack first too, but Git leaves no partial index
	// behind; go-git's older saves could, and a pack placed next to an
	// index that does not match it would be listed through that index.
	finals := []struct{ temp, final string }{
		{w.fw.Name(), packPath},
		{revTemp, revPath},
		{idxTemp, idxPath},
	}
	for _, f := range finals {
		if f.temp == "" {
			continue
		}
		exists, err := fileExists(w.fs, f.final)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		same, err := sameContents(w.fs, f.temp, f.final)
		if errors.Is(err, errVanished) {
			// The final went away, as a repack removes a redundant pack,
			// so place renames the temporary file instead.
			continue
		}
		if err != nil {
			return err
		}
		if !same {
			return differ(f.temp, f.final)
		}
		checked[f.final] = true
	}

	// The marker is placed just before the pack, and only when this writer
	// renames the pack into place. A pack visible without its marker looks
	// ordinary, so whatever the promisor remote withheld would read as
	// corruption until the marker landed; crashing in that window must not be
	// able to produce a repository git refuses to gc.
	//
	// A pack already on disk is left exactly as it is, marked or not. Packs
	// are content addressed, so the same hash means the same objects, and
	// nothing is missing that was not missing before; marking it now would
	// newly declare the repository a partial clone on the strength of a
	// duplicate. place decides which it is when it gets to the pack: a pack
	// it finds in place is left unmarked, and one that went away after the
	// check of every final is placed anew with its marker. One another
	// writer places after place found it missing is marked too, as the
	// marker goes in before the rename that then finds the pack there.
	var markPack func() error
	if w.promisor != nil {
		markPack = func() error {
			promisorTemp, err := stage("tmp_promisor_", func(f io.Writer) error {
				_, err := io.WriteString(f, *w.promisor)
				return err
			})
			if err != nil {
				return err
			}
			// The marker keeps the 0600 its temporary file was created with,
			// as index-pack --promisor creates it[1]. fetch-pack rewrites an
			// existing marker in place[2], which a read-only one would refuse.
			//
			// [1]: https://github.com/git/git/blob/v2.54.0/builtin/index-pack.c#L1557-L1587
			// [2]: https://github.com/git/git/blob/v2.54.0/pack-write.c#L601-L614
			return place(promisorTemp, base+promisorExt, true, nil)
		}
	}

	if err := place(w.fw.Name(), packPath, false, markPack); err != nil {
		return err
	}
	if err := place(revTemp, revPath, false, nil); err != nil {
		return err
	}
	return place(idxTemp, idxPath, false, nil)
}

// maxVanishedRetries is how many times a save places a final anew that went
// away before it was compared, as Git's finalize_object_file does.
const maxVanishedRetries = 6

// errVanished reports that a file sameContents was to compare against is not
// there, as Git's check_collision reports CHECK_COLLISION_DEST_VANISHED.
var errVanished = errors.New("file vanished")

// sameContents reports whether the files a and b in fs hold the same bytes.
// It returns errVanished when b does not exist.
func sameContents(fs billy.Filesystem, a, b string) (same bool, err error) {
	fa, err := fs.Open(a)
	if err != nil {
		return false, err
	}
	defer ioutil.CheckClose(fa, &err)
	fb, err := fs.Open(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, errVanished
	}
	if err != nil {
		return false, err
	}
	defer ioutil.CheckClose(fb, &err)

	bufA, bufB := make([]byte, 32*1024), make([]byte, 32*1024)
	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		endA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		endB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		switch {
		case errA != nil && !endA:
			return false, errA
		case errB != nil && !endB:
			return false, errB
		case endA || endB:
			return endA == endB, nil
		}
	}
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

type syncedReader struct {
	w io.Writer
	r io.ReadSeeker

	blocked, done atomic.Uint32
	written, read atomic.Uint64
	news          chan bool
}

func newSyncedReader(w io.Writer, r io.ReadSeeker) *syncedReader {
	return &syncedReader{
		w:    w,
		r:    r,
		news: make(chan bool),
	}
}

func (s *syncedReader) Write(p []byte) (n int, err error) {
	defer func() {
		written := s.written.Add(uint64(n))
		read := s.read.Load()
		if written > read {
			s.wake()
		}
	}()

	n, err = s.w.Write(p)
	return n, err
}

func (s *syncedReader) Read(p []byte) (n int, err error) {
	defer func() { s.read.Add(uint64(n)) }()

	for {
		s.sleep()
		n, err = s.r.Read(p)
		if err == io.EOF && !s.isDone() && n == 0 {
			continue
		}

		break
	}

	return n, err
}

func (s *syncedReader) isDone() bool {
	return s.done.Load() == 1
}

func (s *syncedReader) isBlocked() bool {
	return s.blocked.Load() == 1
}

func (s *syncedReader) wake() {
	if s.isBlocked() {
		s.blocked.Store(0)
		s.news <- true
	}
}

func (s *syncedReader) sleep() {
	read := s.read.Load()
	written := s.written.Load()
	if read >= written {
		s.blocked.Store(1)
		<-s.news
	}
}

func (s *syncedReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		return s.r.Seek(offset, whence)
	}

	p, err := s.r.Seek(offset, whence)
	s.read.Store(uint64(p))

	return p, err
}

func (s *syncedReader) Close() error {
	s.done.Store(1)
	close(s.news)
	return nil
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
