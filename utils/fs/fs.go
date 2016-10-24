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
	//Create opens a file in write-only mode.
	Create(filename string) (File, error)
	//Open opens a file in read-only mode.
	Open(filename string) (File, error)
	Stat(filename string) (FileInfo, error)
	ReadDir(path string) ([]FileInfo, error)
	TempFile(dir, prefix string) (File, error)
	Rename(from, to string) error
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
