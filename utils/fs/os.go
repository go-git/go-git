package fs

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

// OS is a simple FS implementation for the current host filesystem.
type OS struct{}

// NewOS returns a new OS.
func NewOS() FS {
	return &OS{}
}

// Stat returns the filesystem info for a path.
func (o *OS) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// Open returns a ReadSeekCloser for the specified path.
func (o *OS) Open(path string) (ReadSeekCloser, error) {
	return os.Open(path)
}

// ReadDir returns the filesystem info for all the archives under the
// specified path.
func (o *OS) ReadDir(path string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(path)
}

// Join joins the specified elements using the filesystem separator.
func (o *OS) Join(elem ...string) string {
	return filepath.Join(elem...)
}
