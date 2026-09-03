package packhandle

import "github.com/go-git/go-git/v6/internal/sharedfile"

// ReadAtCloser is the file shape returned by Source.Open.
// It is an alias for [sharedfile.ReadAtCloser]; both names
// refer to the same type at compile time.
type ReadAtCloser = sharedfile.ReadAtCloser

// Source describes how to open and size one pack file. Open runs lazily on
// first use and after idle release. Size runs once after its first success.
type Source struct {
	// Open returns a fresh, independently closeable random-access
	// read handle.
	Open func() (ReadAtCloser, error)
	// Size returns the file's size in bytes.
	Size func() (int64, error)
}
