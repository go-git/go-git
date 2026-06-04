package util

import (
	"bytes"
	"testing"
)

func FuzzVariableLengthSize(f *testing.F) {
	f.Add(byte(0x90), []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80})
	f.Add(byte(0x80), []byte{0x01})
	f.Add(byte(0x00), []byte{})

	f.Fuzz(func(_ *testing.T, first byte, tail []byte) {
		_, _ = VariableLengthSize(first, bytes.NewReader(tail))
	})
}

func FuzzDecodeLEB128(f *testing.F) {
	f.Add([]byte{0x01})
	f.Add([]byte{0x80, 0x01})
	f.Add(bytes.Repeat([]byte{0x80}, 12))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _, _ = DecodeLEB128(data)
	})
}
