package packfile

import (
	"bytes"
	"testing"
)

func FuzzReadLength(f *testing.F) {
	f.Add(byte(0x90), []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80})
	f.Add(byte(0x80), []byte{0x01})
	f.Add(byte(0x00), []byte{})

	f.Fuzz(func(_ *testing.T, first byte, tail []byte) {
		s := NewScanner(bytes.NewReader(tail))
		_, _ = s.readLength(first)
	})
}
