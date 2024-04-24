package pktline

import "sync"

var byteSlice = sync.Pool{
	New: func() interface{} {
		var b [MaxSize]byte
		return &b
	},
}

// GetPacketBuffer returns a *[MaxSize]byte that is managed by a
// sync.Pool. The initial slice length will be 65520 (65kb).
//
// After use, the *[MaxSize]byte should be put back into the sync.Pool by
// calling PutByteSlice.
func GetPacketBuffer() *[MaxSize]byte {
	buf := byteSlice.Get().(*[MaxSize]byte)
	return buf
}

// PutPacketBuffer puts buf back into its sync.Pool.
func PutPacketBuffer(buf *[MaxSize]byte) {
	byteSlice.Put(buf)
}
