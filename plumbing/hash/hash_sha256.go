package hash

import (
	"bytes"
	"encoding/hex"
)

type SHA256Hash struct {
	hash [SHA256Size]byte
}

func (ih SHA256Hash) Size() int {
	return len(ih.hash)
}

func (ih SHA256Hash) IsZero() bool {
	var empty SHA256Hash
	return ih == empty
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
