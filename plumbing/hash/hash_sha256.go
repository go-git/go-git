package hash

import (
	"bytes"
	"encoding/hex"
	"io"
)

type SHA256Hash struct {
	hash [SHA256Size]byte
}

func (ih SHA256Hash) Size() int {
	return len(ih.hash)
}

func (ih SHA256Hash) IsZero() bool {
	return ih == zeroSHA256
}

func (ih SHA256Hash) String() string {
	return hex.EncodeToString(ih.hash[:])
}

func (ih SHA256Hash) Bytes() []byte {
	return ih.hash[:]
}

func (ih SHA256Hash) Compare(in []byte) int {
	return bytes.Compare(ih.hash[:], in)
}

func (ih SHA256Hash) HasPrefix(prefix []byte) bool {
	return bytes.HasPrefix(ih.hash[:], prefix)
}

func (ih *SHA256Hash) Write(in []byte) (int, error) {
	return copy(ih.hash[:], in), nil
}

func (ih *SHA256Hash) FromReaderAt(r io.ReaderAt, off int64) (int, error) {
	return r.ReadAt(ih.hash[:], off)
}

func (ih *SHA256Hash) FromReader(r io.Reader) (int, error) {
	return io.ReadFull(r, ih.hash[:])
}
