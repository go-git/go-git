package pktline

import "sync"

var byteSlice = sync.Pool{
	New: func() interface{} {
		var b [MaxPacketSize]byte
		return &b
	},
}

// GetPacketBuffer returns a *[MaxPacketSize]byte that is managed by a
// sync.Pool. The initial slice length will be 65520 (65kb).
//
// After use, the *[MaxPacketSize]byte should be put back into the sync.Pool by
// calling PutByteSlice.
func GetPacketBuffer() *[MaxPacketSize]byte {
	buf := byteSlice.Get().(*[MaxPacketSize]byte)
	return buf
}

// PutPacketBuffer puts buf back into its sync.Pool.
func PutPacketBuffer(buf *[MaxPacketSize]byte) {
	byteSlice.Put(buf)
}
