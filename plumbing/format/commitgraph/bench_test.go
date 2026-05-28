package commitgraph_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/commitgraph"
)

// benchShape parameterises buildBenchGraph so the parse-side and
// walk-side benchmarks can share a single graph builder while still
// exercising distinct chunk-shape combinations.
type benchShape struct {
	numCommits int
	hasGenV2   bool
	allOctopus bool // every commit (after the first three) has 3 parents
}

// BenchmarkOpenFileIndex anchors OpenFileIndex along the two dimensions
// that move parse cost: chunk count (v1 graphs lack the GDA2 chunk) and
// commit count (parse must stay O(numChunks); a refactor that walks
// per-commit data would surface as a jump on the large case).
//
// small/v1 vs small/v2 measures the cost of one extra TOC entry plus
// one extra verifyChunkSizes branch; large/v2 vs small/v2 confirms the
// O(numChunks) property — ns drift is inside noise but allocs/op staying
// flat is the actual anchor.
func BenchmarkOpenFileIndex(b *testing.B) {
	cases := []struct {
		name  string
		shape benchShape
	}{
		{"small/v1", benchShape{numCommits: 100}},
		{"small/v2", benchShape{numCommits: 100, hasGenV2: true}},
		{"large/v2", benchShape{numCommits: 100_000, hasGenV2: true}},
	}
	for _, tc := range cases {
		raw := buildBenchGraph(b, tc.shape)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				fi, err := commitgraph.OpenFileIndex(
					discardCloseReader{bytes.NewReader(raw)})
				if err != nil {
					b.Fatal(err)
				}
				_ = fi.Close()
			}
		})
	}
}

// BenchmarkGetCommitDataByIndex anchors per-commit walk cost. The EDGE-
// chunk cap introduced by this branch only fires from the octopus path
// inside GetCommitDataByIndex, so a parse-time benchmark cannot
// regression-detect it; v2/octopus exists to keep the per-edge cost
// observable.
//
// The GDO2 overflow-cap branch was tried as a fourth sub-bench at 100%
// fire rate and produced numbers indistinguishable from v2/baseline
// (175 µs / 11 996 allocs in both, three runs). The per-overflow work
// is one ReadAt against an in-memory bytes.Reader plus an 8-byte buffer
// that escape analysis keeps on the stack; the cost sits beneath the
// bench's noise floor and would only become visible behind a slow-disk
// wrapper. Correctness for both the rejection and the happy-path
// overflow read is covered by dedicated unit tests.
func BenchmarkGetCommitDataByIndex(b *testing.B) {
	cases := []struct {
		name  string
		shape benchShape
	}{
		{"v1", benchShape{numCommits: 1000}},
		{"v2/baseline", benchShape{numCommits: 1000, hasGenV2: true}},
		{"v2/octopus", benchShape{numCommits: 1000, hasGenV2: true, allOctopus: true}},
	}
	for _, tc := range cases {
		raw := buildBenchGraph(b, tc.shape)
		fi, err := commitgraph.OpenFileIndex(
			discardCloseReader{bytes.NewReader(raw)})
		if err != nil {
			b.Fatal(err)
		}
		n := fi.MaximumNumberOfHashes()
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				for i := range n {
					if _, err := fi.GetCommitDataByIndex(i); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
		_ = fi.Close()
	}
}

func buildBenchGraph(b *testing.B, s benchShape) []byte {
	b.Helper()
	mem := commitgraph.NewMemoryIndex()
	hashes := make([]plumbing.Hash, s.numCommits)
	for i := range s.numCommits {
		hashes[i] = plumbing.NewHash(fmt.Sprintf("%040x", uint64(i+1)))
	}
	for i := range s.numCommits {
		cd := &commitgraph.CommitData{
			TreeHash:   hashes[i],
			Generation: uint64(i + 1),
		}
		switch {
		case s.allOctopus && i >= 3:
			cd.ParentHashes = []plumbing.Hash{hashes[i-1], hashes[i-2], hashes[i-3]}
		case i > 0:
			cd.ParentHashes = []plumbing.Hash{hashes[i-1]}
		}
		if s.hasGenV2 {
			cd.GenerationV2 = uint64(i + 1)
		}
		mem.Add(hashes[i], cd)
	}
	var buf bytes.Buffer
	if err := commitgraph.NewEncoder(&buf).Encode(mem); err != nil {
		b.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}
