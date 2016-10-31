// Package fs interace and implementations used by storage/filesystem
package fs

import (
	"errors"
	"io"
	"os"
)

var (
	ErrClosed       = errors.New("file: Writing on closed file.")
	ErrReadOnly     = errors.New("this is a read-only filesystem")
	ErrNotSupported = errors.New("feature not supported")
)

type Filesystem interface {
	Create(filename string) (File, error)
	Open(filename string) (File, error)
	OpenFile(filename string, flag int, perm os.FileMode) (File, error)
	Stat(filename string) (FileInfo, error)
	ReadDir(path string) ([]FileInfo, error)
	TempFile(dir, prefix string) (File, error)
	Rename(from, to string) error
	Remove(filename string) error
	Join(elem ...string) string
	Dir(path string) Filesystem
	Base() string
}

type File interface {
	Filename() string
	IsClosed() bool
	io.Writer
	io.Reader
	io.Seeker
	io.Closer
}

type FileInfo os.FileInfo

type BaseFile struct {
	BaseFilename string
	Closed       bool
}

func (f *BaseFile) Filename() string {
	return f.BaseFilename
}

func (f *BaseFile) IsClosed() bool {
	return f.Closed
}
