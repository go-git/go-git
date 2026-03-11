package idxfile_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

func FuzzLazyIndex(f *testing.F) {
	idx3 := buildMinimalIdx(3, 20)
	rev3 := buildMinimalRev(3, 20)
	f.Add(idx3, rev3)

	idx0 := buildMinimalIdx(0, 20)
	rev0 := buildMinimalRev(0, 20)
	f.Add(idx0, rev0)

	raw := bytes.NewBufferString(fixtureLarge4GB)
	if fixtureBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, raw)); err == nil {
		f.Add(fixtureBytes, rev3)
	}

	f.Add([]byte{0xff, 't', 'O', 'c', 0, 0, 0, 2}, []byte{})

	f.Add([]byte{}, []byte{})

	f.Fuzz(func(_ *testing.T, idxData, revData []byte) {
		var packHash plumbing.Hash

		// Try to extract a plausible pack checksum from the idx data.
		// For SHA1 (hashSize=20): packChecksum is at len-40.
		// For SHA256 (hashSize=32): packChecksum is at len-64.
		for _, hs := range []int{20, 32} {
			if len(idxData) >= hs*2 {
				packHash.ResetBySize(hs)
				_, _ = packHash.Write(idxData[len(idxData)-hs*2 : len(idxData)-hs])
			}
		}

		var rev nopCloserReaderAt
		if len(revData) > 0 {
			rev = nopCloserReaderAt{bytes.NewReader(revData)}
		}

		idx, err := idxfile.NewLazyIndex(
			nopCloserReaderAt{bytes.NewReader(idxData)},
			rev,
			packHash,
		)
		if err != nil {
			// Expected for most fuzz inputs.
			return
		}
		defer idx.Close()

		// Exercise all Index methods — none should panic.
		testHash := plumbing.NewHash("abcdef1234567890abcdef1234567890abcdef12")
		_, _ = idx.Contains(testHash)
		_, _ = idx.FindOffset(testHash)
		_, _ = idx.FindCRC32(testHash)
		_, _ = idx.FindHash(42)
		_, _ = idx.Count()

		if iter, err := idx.Entries(); err == nil {
			for range 100 {
				if _, err := iter.Next(); err != nil {
					break
				}
			}
			_ = iter.Close()
		}

		if iter, err := idx.EntriesByOffset(); err == nil {
			for range 100 {
				if _, err := iter.Next(); err != nil {
					break
				}
			}
			_ = iter.Close()
		}
	})
}
