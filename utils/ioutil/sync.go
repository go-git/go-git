package ioutil

import (
	"io"

	"github.com/go-git/go-git/v6/utils/sync"
)

// CopyBufferPool calls io.CopyBuffer and uses a buffer from sync.GetByteSlice,
// to reduce the complexity when using it while avoiding the allocation
// of a new buffer per call.
func CopyBufferPool(dst io.Writer, src io.Reader) (n int64, err error) {
	buf := sync.GetByteSlice()
	n, err = io.CopyBuffer(dst, src, *buf)
	sync.PutByteSlice(buf)

	return n, err
}
