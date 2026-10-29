// Package sync provides sync.Pool-based utilities for reusing objects.
package sync

import (
	"bufio"
	"io"
	"sync"
)

var bufioReader = sync.Pool{
	New: func() any {
		return bufio.NewReader(nil)
	},
}

// GetBufioReader returns a *bufio.Reader that is managed by a sync.Pool.
// Returns a bufio.Reader that is reset with reader and ready for use.
//
// After use, the *bufio.Reader should be put back into the sync.Pool
// by calling PutBufioReader.
func GetBufioReader(reader io.Reader) *bufio.Reader {
	r := bufioReader.Get().(*bufio.Reader)
	r.Reset(reader)
	return r
}

// PutBufioReader puts reader back into its sync.Pool. It drops the reference
// to the underlying io.Reader first, so the pool does not keep that reader,
// and anything it holds, alive. reader must not be used afterwards.
func PutBufioReader(reader *bufio.Reader) {
	if reader == nil {
		return
	}
	reader.Reset(nil)
	bufioReader.Put(reader)
}
