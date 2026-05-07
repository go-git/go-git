package packfile

import (
	"bytes"
	"testing"
)

func FuzzDecodeLEB128(f *testing.F) {
	f.Add([]byte{0x01})
	f.Add([]byte{0x80, 0x01})
	f.Add(bytes.Repeat([]byte{0x80}, 12))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _, _ = decodeLEB128(data)
	})
}

func FuzzDecodeLEB128ByteReader(f *testing.F) {
	f.Add([]byte{0x01})
	f.Add([]byte{0x80, 0x01})
	f.Add(bytes.Repeat([]byte{0x80}, 12))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = decodeLEB128ByteReader(bytes.NewReader(data))
	})
}
