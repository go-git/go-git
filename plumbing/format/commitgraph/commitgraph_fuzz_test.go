package commitgraph

import (
	"bytes"
	encbin "encoding/binary"
	"fmt"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/binary"
)

// fuzzSeedMismatchedChunkCount builds a commit-graph file whose header declares
// fewer chunks than the chunk table-of-contents actually holds, with no
// terminator entry. It exercises the bound that pins iteration to the
// declared count.
func fuzzSeedMismatchedChunkCount(declared uint8, entries []ChunkType) []byte {
	var buf bytes.Buffer
	buf.WriteString("CGPH")
	buf.WriteByte(1)        // header version
	buf.WriteByte(1)        // hash version
	buf.WriteByte(declared) // declared num_chunks
	buf.WriteByte(0)        // base graphs
	offset := uint64(8 + len(entries)*12)
	for _, c := range entries {
		buf.Write(c.Signature())
		_ = binary.WriteUint64(&buf, offset)
		offset += 16
	}
	return buf.Bytes()
}

func FuzzOpenFileIndex(f *testing.F) {
	// Seed from real commit-graph fixture when available.
	for _, fix := range fixtures.ByTag("commit-graph") {
		if dotgit, err := fix.DotGit(); err == nil {
			path := dotgit.Join("objects", "info", "commit-graph")
			if fh, err := dotgit.Open(path); err == nil {
				if data, err := io.ReadAll(fh); err == nil {
					f.Add(data)
				}
				_ = fh.Close()
			}
		}
	}

	// Minimal header: 4-byte signature + version(1) + hash-version(1) +
	// number-of-chunks(1) + number-of-base-graphs(1).
	f.Add([]byte("CGPH\x01\x01\x00\x00"))
	// Header declares fewer chunks than the chunk table-of-contents
	// holds, so the parser must iterate by the declared count and
	// reject the missing terminator.
	f.Add(fuzzSeedMismatchedChunkCount(2, []ChunkType{
		OIDFanoutChunk, OIDLookupChunk, CommitDataChunk,
		GenerationDataChunk, ExtraEdgeListChunk,
	}))
	// Generation-data overflow pointer past the GDO2 chunk; mirrors
	// the shape rejected by TestGetCommitDataRejectsGenerationOverflowPastChunk.
	f.Add(fuzzSeedGenerationOverflowPastChunk())
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		idx, err := OpenFileIndex(struct {
			io.ReaderAt
			io.Closer
		}{
			bytes.NewReader(data),
			io.NopCloser(nil),
		})
		if err != nil {
			return
		}
		defer idx.Close()

		// Walk the index to exercise fanout-driven offset math in
		// GetCommitDataByIndex and the OID lookup / generation-data paths.
		hashes := idx.Hashes()
		const maxIters = 4096
		n := min(len(hashes), maxIters)
		for i := range n {
			_, _ = idx.GetIndexByHash(hashes[i])
			_, _ = idx.GetHashByIndex(uint32(i))
			_, _ = idx.GetCommitDataByIndex(uint32(i))
		}
	})
}

// fuzzSeedGenerationOverflowPastChunk encodes a one-commit graph whose
// GenerationV2 value forces overflow encoding (>= 0x80000000 and above
// MaxUint32), then surgically rewrites the GDA2 entry to point at the
// largest representable overflow index. GetCommitDataByIndex must
// reject before reading past the single-entry GDO2 chunk.
func fuzzSeedGenerationOverflowPastChunk() []byte {
	mem := NewMemoryIndex()
	mem.Add(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		&CommitData{
			TreeHash:     plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Generation:   1,
			GenerationV2: 0x100000001,
		})
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(mem); err != nil {
		return nil
	}
	raw := buf.Bytes()

	numChunks := int(raw[6])
	const tocBase = 8
	const tocEntrySize = 12
	for i := range numChunks {
		base := tocBase + i*tocEntrySize
		if string(raw[base:base+4]) == "GDA2" {
			gda2Offset := int(encbin.BigEndian.Uint64(raw[base+4:]))
			encbin.BigEndian.PutUint32(raw[gda2Offset:], 0x80000000|0x7FFFFFFF)
			break
		}
	}
	return raw
}

// FuzzEncoderRoundTrip drives the encoder with a small randomly-shaped
// MemoryIndex and asserts the produced byte stream parses back. Catches
// drift between the chunk-count cap on the writer side (byte 6 = uint8)
// and the on-disk header byte the reader extracts.
func FuzzEncoderRoundTrip(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint8(0), uint8(0))
	f.Add(uint8(5), uint8(3), uint8(2))
	f.Add(uint8(255), uint8(0), uint8(0))

	f.Fuzz(func(t *testing.T, numCommits, octopusMod, gv2Mod uint8) {
		// Cap fuzzer-suggested numCommits to keep iterations cheap.
		const maxN = 64
		n := int(numCommits) % (maxN + 1)
		mem := NewMemoryIndex()

		hashes := make([]plumbing.Hash, n)
		for i := range n {
			hashes[i] = plumbing.NewHash(fmt.Sprintf("%040x", uint64(i+1)))
		}
		for i := range n {
			cd := &CommitData{
				TreeHash:   hashes[i],
				Generation: uint64(i + 1),
			}
			// Octopus: 3 parents on selected commits.
			if octopusMod > 1 && i >= 3 && i%int(octopusMod) == 0 {
				cd.ParentHashes = []plumbing.Hash{hashes[i-1], hashes[i-2], hashes[i-3]}
			} else if i >= 1 {
				cd.ParentHashes = []plumbing.Hash{hashes[i-1]}
			}
			// GenerationV2: force overflow encoding on selected commits.
			if gv2Mod > 0 && i%(int(gv2Mod)+1) == 0 {
				cd.GenerationV2 = 0x100000001
			} else {
				cd.GenerationV2 = uint64(i + 1)
			}
			mem.Add(hashes[i], cd)
		}

		var buf bytes.Buffer
		if err := NewEncoder(&buf).Encode(mem); err != nil {
			// Legitimate rejections (e.g. ErrTooManyChunks) are valid.
			return
		}
		out, err := OpenFileIndex(struct {
			io.ReaderAt
			io.Closer
		}{
			bytes.NewReader(buf.Bytes()),
			io.NopCloser(nil),
		})
		if err != nil {
			t.Fatalf("round-trip parse failed: %v", err)
		}
		t.Cleanup(func() { _ = out.Close() })

		// Walk to exercise EDGE / GDA2 / GDO2 paths bounded by the new
		// chunk-size assertions.
		for i := range n {
			if _, err := out.GetCommitDataByIndex(uint32(i)); err != nil {
				t.Fatalf("commit %d: %v", i, err)
			}
		}
	})
}
