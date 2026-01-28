package dotgit

import (
	"crypto"
	"errors"
	"fmt"
	"hash"
	"io"
	"runtime"
	"sync/atomic"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
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
	parser   *packfile.Parser
	writer   *idxfile.Writer
	result   chan error
}

func newPackWrite(fs billy.Filesystem) (*PackWriter, error) {
	fw, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_pack_")
	if err != nil {
		return nil, err
	}

	fr, err := fs.Open(fw.Name())
	if err != nil {
		return nil, err
	}

	writer := &PackWriter{
		fs:     fs,
		fw:     fw,
		fr:     fr,
		synced: newSyncedReader(fw, fr),
		result: make(chan error),
	}

	go writer.buildIndex()
	return writer, nil
}

func (w *PackWriter) buildIndex() {
	w.writer = new(idxfile.Writer)
	var err error

	w.parser = packfile.NewParser(w.synced, packfile.WithScannerObservers(w.writer))

	h, err := w.parser.Parse()
	if err != nil {
		w.result <- err
		return
	}

	w.checksum = h
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
func (w *PackWriter) Close() error {
	defer func() {
		if w.Notify != nil && w.writer != nil && w.writer.Finished() {
			w.Notify(w.checksum, w.writer)
		}

		close(w.result)
	}()

	if err := w.synced.Close(); err != nil {
		return err
	}

	if err := w.waitBuildIndex(); err != nil {
		return err
	}

	if err := w.fr.Close(); err != nil {
		return err
	}

	if err := w.fw.Close(); err != nil {
		return err
	}

	if w.writer == nil || !w.writer.Finished() {
		return w.clean()
	}

	return w.save()
}

func (w *PackWriter) clean() error {
	return w.fs.Remove(w.fw.Name())
}

func (w *PackWriter) save() error {
	base := w.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s", w.checksum))

	idx, err := w.fs.Create(fmt.Sprintf("%s.idx", base))
	if err != nil {
		return err
	}

	h := crypto.SHA1.New()
	if w.checksum.Size() == crypto.SHA256.Size() {
		h = crypto.SHA256.New()
	}

	if err := w.encodeIdx(idx); err != nil {
		_ = idx.Close()
		return err
	}

	if err := idx.Close(); err != nil {
		return err
	}
	fixPermissions(w.fs, fmt.Sprintf("%s.idx", base))

	rev, err := w.fs.Create(fmt.Sprintf("%s.rev", base))
	if err != nil {
		return err
	}

	if err := w.encodeRev(rev, h); err != nil {
		_ = rev.Close()
		return err
	}

	if err := rev.Close(); err != nil {
		return err
	}
	fixPermissions(w.fs, fmt.Sprintf("%s.rev", base))

	packPath := fmt.Sprintf("%s.pack", base)
	if err := w.fs.Rename(w.fw.Name(), packPath); err != nil {
		return err
	}
	fixPermissions(w.fs, packPath)

	return nil
}

func (w *PackWriter) encodeIdx(writer io.Writer) error {
	idx, err := w.writer.Index()
	if err != nil {
		return err
	}

	e := idxfile.NewEncoder(writer)
	_, err = e.Encode(idx)
	return err
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
		written := s.written.Add(uint64(n)) //nolint:gosec // G115: n is bytes written, always non-negative
		read := s.read.Load()
		if written > read {
			s.wake()
		}
	}()

	n, err = s.w.Write(p)
	return n, err
}

func (s *syncedReader) Read(p []byte) (n int, err error) {
	defer func() { s.read.Add(uint64(n)) }() //nolint:gosec // G115: n is bytes read, always non-negative

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
	s.read.Store(uint64(p)) //nolint:gosec // G115: p is file position, always non-negative

	return p, err
}

func (s *syncedReader) Close() error {
	s.done.Store(1)
	close(s.news)
	return nil
}

type ObjectWriter struct {
	objfile.Writer
	fs billy.Filesystem
	f  billy.File
}

func newObjectWriter(fs billy.Filesystem) (*ObjectWriter, error) {
	f, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_obj_")
	if err != nil {
		return nil, err
	}

	return &ObjectWriter{
		Writer: (*objfile.NewWriter(f)),
		fs:     fs,
		f:      f,
	}, nil
}

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

	if err := w.fs.Rename(w.f.Name(), file); err != nil {
		return err
	}
	fixPermissions(w.fs, file)

	return nil
}

func fixPermissions(fs billy.Filesystem, path string) {
	if runtime.GOOS == "windows" {
		return
	}

	if chmodFS, ok := fs.(billy.Chmod); ok {
		if err := chmodFS.Chmod(path, 0o444); err != nil {
			trace.General.Printf("failed to chmod %s: %v", path, err)
		}
	}
}
