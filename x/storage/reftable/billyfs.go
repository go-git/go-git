package reftable

import (
	"cmp"
	"errors"
	"io"
	"io/fs"
	"os"

	billy "github.com/go-git/go-billy/v6"
	rt "github.com/hanwen/reftable"
)

// BillyStorage adapts a billy.Filesystem to reftable.Storage. Files are
// stored under a single directory dir on the underlying filesystem; the
// caller is responsible for ensuring dir exists (NewBillyStorage will
// create it).
type BillyStorage struct {
	fs  billy.Filesystem
	dir string
}

// NewBillyStorage returns a reftable.Storage backed by fs, with all reftable
// files under dir.
func NewBillyStorage(fs billy.Filesystem, dir string) (*BillyStorage, error) {
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &BillyStorage{fs: fs, dir: dir}, nil
}

func (s *BillyStorage) path(name string) string {
	return s.fs.Join(s.dir, name)
}

// LockForWrite implements reftable.Storage by creating "<name>.lock"
// exclusively. The returned writer renames the lock to <name> on Commit
// and removes it on Close.
func (s *BillyStorage) LockForWrite(name string) (rt.AtomicWriter, error) {
	final := s.path(name)
	lock := final + ".lock"
	f, err := s.fs.OpenFile(lock, os.O_EXCL|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &billyWriter{fs: s.fs, file: f, tmpName: lock, finalName: final}, nil
}

// Update implements reftable.Storage. The returned writer writes to a
// temp file under dir; Commit renames it to name.
func (s *BillyStorage) Update(name string) (rt.AtomicWriter, error) {
	final := s.path(name)
	f, err := s.fs.TempFile(s.dir, name+".*.tmp")
	if err != nil {
		return nil, err
	}
	return &billyWriter{fs: s.fs, file: f, tmpName: f.Name(), finalName: final}, nil
}

// ReadDir implements reftable.Storage.
func (s *BillyStorage) ReadDir() ([]fs.DirEntry, error) {
	return s.fs.ReadDir(s.dir)
}

// Remove implements reftable.Storage.
func (s *BillyStorage) Remove(name string) error {
	err := s.fs.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// OpenBlockSource implements reftable.Storage.
func (s *BillyStorage) OpenBlockSource(name string) (rt.BlockSource, error) {
	f, err := s.fs.Open(s.path(name))
	if err != nil {
		return nil, err
	}
	fi, err := s.fs.Stat(s.path(name))
	if err != nil {
		f.Close()
		return nil, err
	}
	return &billyBlockSource{f: f, sz: uint64(fi.Size())}, nil
}

var _ rt.Storage = (*BillyStorage)(nil)

// billyWriter implements reftable.AtomicWriter on top of a billy.File.
// It writes to tmpName and on Commit renames it to finalName. Close
// (without prior Commit) removes the temp file.
type billyWriter struct {
	fs        billy.Filesystem
	file      billy.File
	tmpName   string
	finalName string
	committed bool
}

func (w *billyWriter) Write(p []byte) (int, error) { return w.file.Write(p) }

func (w *billyWriter) Name() string {
	if w.committed {
		return baseName(w.finalName)
	}
	return baseName(w.tmpName)
}

func (w *billyWriter) Close() error {
	if w.committed || w.file == nil {
		return nil
	}
	err1 := w.file.Close()
	err2 := w.fs.Remove(w.tmpName)
	w.file = nil
	if errors.Is(err2, os.ErrNotExist) {
		err2 = nil
	}
	return cmp.Or(err1, err2)
}

func (w *billyWriter) Commit() error {
	if w.committed {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	if err := w.fs.Rename(w.tmpName, w.finalName); err != nil {
		return err
	}
	w.file = nil
	w.committed = true
	return nil
}

func (w *billyWriter) Committed() bool { return w.committed }

// baseName returns the final path component, using "/" since billy paths
// use forward slashes.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// billyBlockSource adapts a billy.File to reftable.BlockSource.
type billyBlockSource struct {
	f  billy.File
	sz uint64
}

func (b *billyBlockSource) Size() uint64 { return b.sz }

func (b *billyBlockSource) ReadBlock(off uint64, size int) ([]byte, error) {
	if off > b.sz {
		return nil, io.EOF
	}
	if off+uint64(size) > b.sz {
		size = int(b.sz - off)
	}
	buf := make([]byte, size)
	n, err := b.f.ReadAt(buf, int64(off))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (b *billyBlockSource) Close() error { return b.f.Close() }
