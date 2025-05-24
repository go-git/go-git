package sync

import (
	"bytes"
	"sync"
)

var (
	byteSlice = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 16*1024)
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
// The initial slice length will be 16384 (16kb).
//
// After use, the *[]byte should be put back into the sync.Pool
// by calling PutByteSlice.
func GetByteSlice() *[]byte {
	buf := byteSlice.Get().(*[]byte)
	return buf
}

// PutByteSlice puts buf back into its sync.Pool.
func PutByteSlice(buf *[]byte, used int) {
	if buf == nil {
		return
	}

	b := *buf
	if used <= 0 {
		used = cap(b)
	}

	n := min(int(used), cap(b))
	for i := 0; i < n; i++ {
		b[i] = 0
	}

	byteSlice.Put(&b)
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
