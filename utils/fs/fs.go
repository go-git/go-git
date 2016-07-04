package fs

import (
	"io"
	"os"
)

// FS interface represent an abstracted filesystem, so you can
// use NewRepositoryFromFS from any medium.
type FS interface {
	Stat(path string) (os.FileInfo, error)
	Open(path string) (ReadSeekCloser, error)
	ReadDir(path string) ([]os.FileInfo, error)
	Join(elem ...string) string
}

// ReadSeekCloser is a Reader, Seeker and Closer.
type ReadSeekCloser interface {
	io.ReadCloser
	io.Seeker
}
