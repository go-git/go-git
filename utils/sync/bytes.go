package sync

import (
	"bytes"
	"sync"
)

var (
	size = 32 * 1024

	byteSlice = sync.Pool{
		New: func() interface{} {
			b := make([]byte, size)
			return &b
		},
	}
	bytesBuffer = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(nil)
		},
	}
)

// GetByteSlice returns a *[]byte that is managed by a sync.Pool.
// The initial slice length will be 32768 (32kb).
//
// After use, the *[]byte should be put back into the sync.Pool
// by calling PutByteSlice.
func GetByteSlice() *[]byte {
	buf := byteSlice.Get().(*[]byte)
	b := *buf
	if len(b) < size {
		b = b[:cap(b)]
	}

	// zero out the array contents.
	for i := 0; i < len(b); i++ {
		b[i] = 0
	}

	return &b
}

// PutByteSlice puts buf back into its sync.Pool.
func PutByteSlice(buf *[]byte) {
	if buf == nil {
		return
	}

	byteSlice.Put(buf)
}

// GetBytesBuffer returns a *bytes.Buffer that is managed by a sync.Pool.
// Returns a buffer that is reset and ready for use.
//
// After use, the *bytes.Buffer should be put back into the sync.Pool
// by calling PutBytesBuffer.
func GetBytesBuffer() *bytes.Buffer {
	buf := bytesBuffer.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutBytesBuffer puts buf back into its sync.Pool.
func PutBytesBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	bytesBuffer.Put(buf)
}
