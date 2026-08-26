package index

import (
	"bytes"
	"crypto"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	gogithash "github.com/go-git/go-git/v6/plumbing/hash"
)

// FuzzDecoder must stay self-contained: the OSS-Fuzz native build extracts this
// function into a non-test file, so it can only reference production symbols and
// the standard library, never helpers defined in _test.go files.
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

	f.Fuzz(func(t *testing.T, data []byte) {
		// Decode under both object formats so the 20-byte and 32-byte hash
		// paths are exercised.
		for _, algo := range []crypto.Hash{crypto.SHA1, crypto.SHA256} {
			idx := &Index{}
			if err := NewDecoder(bytes.NewReader(data), gogithash.New(algo), WithSkipHash()).Decode(idx); err != nil {
				continue
			}

			// A successfully decoded index must re-encode to a canonical form
			// that decodes and re-encodes byte-identically. A plain
			// "does not panic" check cannot see silent truncation or
			// encode/decode asymmetry; this stability invariant can.
			var enc bytes.Buffer
			if err := NewEncoder(&enc, gogithash.New(algo)).Encode(idx); err != nil {
				// A decoded index the encoder rejects is not a round-trip
				// violation.
				continue
			}

			reIdx := &Index{}
			if err := NewDecoder(bytes.NewReader(enc.Bytes()), gogithash.New(algo), WithSkipHash()).Decode(reIdx); err != nil {
				t.Fatalf("re-decode of encoded index failed (algo=%v): %v", algo, err)
			}

			var reEnc bytes.Buffer
			if err := NewEncoder(&reEnc, gogithash.New(algo)).Encode(reIdx); err != nil {
				t.Fatalf("re-encode failed (algo=%v): %v", algo, err)
			}

			if !bytes.Equal(enc.Bytes(), reEnc.Bytes()) {
				t.Fatalf("encoding not stable across round trip (algo=%v)", algo)
			}
		}
	})
}
