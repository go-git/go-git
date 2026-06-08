package index

import (
	"bytes"
	"crypto"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	gogithash "github.com/go-git/go-git/v6/plumbing/hash"
)

func FuzzDecoder(f *testing.F) {
	// Seed from a real index file when available.
	if dotgit, err := fixtures.Basic().One().DotGit(); err == nil {
		if fh, err := dotgit.Open(dotgit.Join("index")); err == nil {
			if data, err := io.ReadAll(fh); err == nil {
				f.Add(data)
			}
			_ = fh.Close()
		}
	}

	// Minimal DIRC headers for each supported version.
	// 4-byte signature + 4-byte version + 4-byte entry count (0).
	f.Add([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00")) // v2 empty
	f.Add([]byte("DIRC\x00\x00\x00\x03\x00\x00\x00\x00")) // v3 empty
	f.Add([]byte("DIRC\x00\x00\x00\x04\x00\x00\x00\x00")) // v4 empty
	f.Add([]byte{})

	// Seed reaching the TREE extension decoder: DIRC v2 + 0 entries,
	// then a single empty-root TREE entry, then the 20-byte trailing
	// checksum so the extension-loop peek (4+4+hashSize) succeeds.
	treeExt := []byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00" +
		"TREE\x00\x00\x00\x19" + // 4-byte signature + uint32 BE length (25)
		"\x000 0\n") // empty path, 0 entries, 0 subtrees
	treeExt = append(treeExt, make([]byte, 20)...) // root tree hash
	treeExt = append(treeExt, make([]byte, 20)...) // trailing checksum
	f.Add(treeExt)

	f.Fuzz(func(_ *testing.T, data []byte) {
		idx := &Index{}
		h := gogithash.New(crypto.SHA1)
		d := NewDecoder(bytes.NewReader(data), h, WithSkipHash())
		_ = d.Decode(idx)
	})
}
