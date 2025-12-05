//go:build unix

package pack_test

import (
	"crypto"
	"io"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/storage/filesystem/mmap"
)

func newPackScanner(pack, idx, rev billy.File) packHandler[uint64] {
	_, err := pack.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}
	_, err = idx.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}
	_, err = rev.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}

	s, err := mmap.NewPackScanner(crypto.SHA1.Size(), pack, idx, rev)
	if err != nil {
		panic(err)
	}

	return s
}
