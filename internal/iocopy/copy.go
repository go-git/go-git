package iocopy

import (
	"io"
	"sync"
)

// Copy is a variant of io.Copy
// that uses an internal buffer pool
// for optimal copying performance.
func Copy(w io.Writer, r io.Reader) (int64, error) {
	buf := byteSlicePtrPool.Get().(*[]byte)
	n, err := io.CopyBuffer(w, r, *buf)
	byteSlicePtrPool.Put(buf)
	return n, err
}

// Pool of *[]bytes.
// We don't typically use pointers of slices,
// but a sync.Pool prefers pointer-like values.
// Otherwise, conversion from non-pointer to interface{} causes an allocation.
var byteSlicePtrPool = sync.Pool{
	New: func() interface{} {
		buff := make([]byte, 32*1024)
		return &buff
	},
}
