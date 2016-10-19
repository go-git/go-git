package dotgit

import (
	"fmt"
	"io"
	"sync/atomic"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/idxfile"
	"gopkg.in/src-d/go-git.v4/formats/objfile"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

type PackWriter struct {
	Notify func(h core.Hash, i idxfile.Idxfile)

	fs       fs.Filesystem
	fr, fw   fs.File
	synced   *syncedReader
	checksum core.Hash
	index    idxfile.Idxfile
	result   chan error
}

func newPackWrite(fs fs.Filesystem) (*PackWriter, error) {
	fw, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_pack_")
	if err != nil {
		return nil, err
	}

	fr, err := fs.Open(fw.Filename())
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
	s := packfile.NewScanner(w.synced)
	d, err := packfile.NewDecoder(s, nil)
	if err != nil {
		w.result <- err
		return
	}

	checksum, err := d.Decode()
	if err != nil {
		w.result <- err
		return
	}

	w.checksum = checksum
	w.index.PackfileChecksum = checksum
	w.index.Version = idxfile.VersionSupported

	offsets := d.Offsets()
	for h, crc := range d.CRCs() {
		w.index.Add(h, uint64(offsets[h]), crc)
	}

	w.result <- err
}

func (w *PackWriter) Write(p []byte) (n int, err error) {
	return w.synced.Write(p)
}

func (w *PackWriter) Close() error {
	defer func() {
		close(w.result)
	}()

	pipe := []func() error{
		w.synced.Close,
		func() error { return <-w.result },
		w.fr.Close,
		w.fw.Close,
		w.save,
	}

	for _, f := range pipe {
		if err := f(); err != nil {
			return err
		}
	}

	if w.Notify != nil {
		w.Notify(w.checksum, w.index)
	}

	return nil
}

func (w *PackWriter) save() error {
	base := w.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s", w.checksum))
	idx, err := w.fs.Create(fmt.Sprintf("%s.idx", base))
	if err != nil {
		return err
	}

	if err := w.encodeIdx(idx); err != nil {
		return err
	}

	if err := idx.Close(); err != nil {
		return err
	}

	return w.fs.Rename(w.fw.Filename(), fmt.Sprintf("%s.pack", base))
}

func (w *PackWriter) encodeIdx(writer io.Writer) error {
	e := idxfile.NewEncoder(writer)
	_, err := e.Encode(&w.index)
	return err
}

type syncedReader struct {
	w io.Writer
	r io.ReadSeeker

	blocked, done uint32
	written, read uint64
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
		written := atomic.AddUint64(&s.written, uint64(n))
		read := atomic.LoadUint64(&s.read)
		if written > read {
			s.wake()
		}
	}()

	n, err = s.w.Write(p)
	return
}

func (s *syncedReader) Read(p []byte) (n int, err error) {
	defer func() { atomic.AddUint64(&s.read, uint64(n)) }()

	s.sleep()
	n, err = s.r.Read(p)
	if err == io.EOF && !s.isDone() {
		if n == 0 {
			return s.Read(p)
		}

		return n, nil
	}

	return
}

func (s *syncedReader) isDone() bool {
	return atomic.LoadUint32(&s.done) == 1
}

func (s *syncedReader) isBlocked() bool {
	return atomic.LoadUint32(&s.blocked) == 1
}

func (s *syncedReader) wake() {
	if s.isBlocked() {
		//	fmt.Println("wake")
		atomic.StoreUint32(&s.blocked, 0)
		s.news <- true
	}
}

func (s *syncedReader) sleep() {
	read := atomic.LoadUint64(&s.read)
	written := atomic.LoadUint64(&s.written)
	if read >= written {
		atomic.StoreUint32(&s.blocked, 1)
		//	fmt.Println("sleep", read, written)
		<-s.news
	}

}

func (s *syncedReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		return s.r.Seek(offset, whence)
	}

	p, err := s.r.Seek(offset, whence)
	s.read = uint64(p)

	return p, err
}

func (s *syncedReader) Close() error {
	atomic.StoreUint32(&s.done, 1)
	close(s.news)
	return nil
}

type ObjectWriter struct {
	objfile.Writer
	fs fs.Filesystem
	f  fs.File
}

func newObjectWriter(fs fs.Filesystem) (*ObjectWriter, error) {
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
	hash := w.Hash().String()
	file := w.fs.Join(objectsPath, hash[0:2], hash[2:40])

	return w.fs.Rename(w.f.Filename(), file)
}
