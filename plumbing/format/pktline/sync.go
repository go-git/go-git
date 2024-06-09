package pktline

import "sync"

var pktBuffer = sync.Pool{
	New: func() interface{} {
		var b [MaxSize]byte
		return &b
	},
}

// GetBuffer returns a *[MaxSize]byte that is managed by a sync.Pool. The
// initial slice length will be 65520 (65kb).
//
// After use, the *[MaxSize]byte should be put back into the sync.Pool by
// calling PutBuffer.
func GetBuffer() *[MaxSize]byte {
	buf := pktBuffer.Get().(*[MaxSize]byte)
	return buf
}

// PutBuffer puts buf back into its sync.Pool.
func PutBuffer(buf *[MaxSize]byte) {
	pktBuffer.Put(buf)
}
