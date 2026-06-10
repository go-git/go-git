//go:build darwin || linux

package main

import (
	"bytes"
	"io/fs"
	"os"

	"github.com/go-git/go-billy/v6"
	"golang.org/x/sys/unix"
)

// mmapFS wraps a billy.Filesystem so that files opened read-only are backed
// by an mmap'd region instead of regular read syscalls.
type mmapFS struct {
	billy.Filesystem
}

func newMmapFS(fs billy.Filesystem) *mmapFS {
	return &mmapFS{Filesystem: fs}
}

func (m *mmapFS) Open(name string) (billy.File, error) {
	return m.OpenFile(name, os.O_RDONLY, 0)
}

func (m *mmapFS) OpenFile(name string, flag int, perm fs.FileMode) (billy.File, error) {
	f, err := m.Filesystem.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return f, nil
	}

	info, err := f.Stat()
	if err != nil || info.Size() == 0 || info.IsDir() {
		return f, err
	}

	fd, ok := fileDescriptor(f)
	if !ok {
		return f, nil
	}

	data, err := unix.Mmap(int(fd), 0, int(info.Size()), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}

	return &mmapFile{
		name:   name,
		data:   data,
		reader: bytes.NewReader(data),
		file:   f,
		info:   info,
	}, nil
}

func (m *mmapFS) Chroot(path string) (billy.Filesystem, error) {
	sub, err := m.Filesystem.Chroot(path)
	if err != nil {
		return nil, err
	}
	return &mmapFS{Filesystem: sub}, nil
}

func fileDescriptor(f billy.File) (uintptr, bool) {
	type billyFd interface {
		Fd() (uintptr, bool)
	}
	type osFd interface {
		Fd() uintptr
	}
	if bf, ok := f.(billyFd); ok {
		return bf.Fd()
	}
	if of, ok := f.(osFd); ok {
		return of.Fd(), true
	}
	return 0, false
}

// mmapFile is a read-only billy.File backed by an mmap'd region.
type mmapFile struct {
	name   string
	data   []byte
	reader *bytes.Reader
	file   billy.File
	info   fs.FileInfo
	closed bool
}

func (f *mmapFile) Name() string                                 { return f.name }
func (f *mmapFile) Read(p []byte) (int, error)                   { return f.reader.Read(p) }
func (f *mmapFile) ReadAt(p []byte, off int64) (int, error)      { return f.reader.ReadAt(p, off) }
func (f *mmapFile) Seek(offset int64, whence int) (int64, error) { return f.reader.Seek(offset, whence) }
func (f *mmapFile) Stat() (fs.FileInfo, error)                   { return f.info, nil }
func (f *mmapFile) Write(p []byte) (int, error)                  { return 0, billy.ErrReadOnly }
func (f *mmapFile) WriteAt(p []byte, off int64) (int, error)     { return 0, billy.ErrReadOnly }
func (f *mmapFile) Truncate(size int64) error                    { return billy.ErrReadOnly }
func (f *mmapFile) Lock() error                                  { return nil }
func (f *mmapFile) Unlock() error                                { return nil }

func (f *mmapFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	uerr := unix.Munmap(f.data)
	cerr := f.file.Close()
	if uerr != nil {
		return uerr
	}
	return cerr
}
