package pktline

import "sync"

var pktBuffer = sync.Pool{
	New: func() any {
		var b [MaxSize]byte
		return &b
	},
}

// getBuffer returns a *[MaxSize]byte that is managed by a sync.Pool.
//
// After use, the *[MaxSize]byte should be put back into the sync.Pool by
// calling putBuffer.
func getBuffer() *[MaxSize]byte {
	buf := pktBuffer.Get().(*[MaxSize]byte)
	return buf
}

// putBuffer puts buf back into its sync.Pool.
func putBuffer(buf *[MaxSize]byte) {
	if buf == nil {
		return
	}
	pktBuffer.Put(buf)
}
