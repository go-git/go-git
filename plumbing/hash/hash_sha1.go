package hash

import (
	"bytes"
	"crypto"
	"encoding/hex"
	"io"
)

const (
	// CryptoType defines what hash algorithm is being used.
	CryptoType = crypto.SHA1
	// Size defines the amount of bytes the hash yields.
	Size = SHA1Size
	// HexSize defines the strings size of the hash when represented in hexadecimal.
	HexSize = SHA1HexSize
)

type SHA1Hash struct {
	hash [SHA1Size]byte
}

func (ih SHA1Hash) Size() int {
	return len(ih.hash)
}

func (ih SHA1Hash) IsZero() bool {
	return ih == zeroSHA1
}

func (ih SHA1Hash) String() string {
	return hex.EncodeToString(ih.hash[:])
}

func (ih SHA1Hash) Bytes() []byte {
	return ih.hash[:]
}

func (ih SHA1Hash) Compare(in []byte) int {
	return bytes.Compare(ih.hash[:], in)
}

func (ih SHA1Hash) HasPrefix(prefix []byte) bool {
	return bytes.HasPrefix(ih.hash[:], prefix)
}

func (ih *SHA1Hash) Write(in []byte) (int, error) {
	return copy(ih.hash[:], in), nil
}

func (ih *SHA1Hash) FromReaderAt(r io.ReaderAt, off int64) (int, error) {
	return r.ReadAt(ih.hash[:], off)
}

func (ih *SHA1Hash) FromReader(r io.Reader) (int, error) {
	return io.ReadFull(r, ih.hash[:])
}
