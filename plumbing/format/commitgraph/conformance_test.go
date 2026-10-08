package commitgraph_test

import (
	"bytes"
	encbin "encoding/binary"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

// TestOpenFileIndexRejectsNonMonotonicFanout verifies the OID fanout is
// validated to be monotonically non-decreasing. Canonical Git:
// graph_read_oid_fanout rejects "commit-graph fanout values out of order"
// (commit-graph.c v2.54.0).
func (s *CommitgraphSuite) TestOpenFileIndexRejectsNonMonotonicFanout() {
	raw, err := buildSimpleEncoded()
	s.Require().NoError(err)

	oidfOff, ok := findTOCOffset(raw, []byte("OIDF"))
	s.Require().True(ok, "OIDF TOC entry not present")

	// The single commit's OID begins with 0xaa, so fanout[0] is 0. Set it
	// to a value greater than fanout[1] (still 0), breaking monotonicity.
	encbin.BigEndian.PutUint32(raw[int(oidfOff):], 5)

	_, err = openIndexBytes(raw)
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile,
		"expected ErrMalformedCommitGraphFile for non-monotonic fanout")
}

func (s *CommitgraphSuite) TestOpenFileIndexIgnoresHeaderBaseGraphCount() {
	raw, err := buildSimpleEncoded()
	s.Require().NoError(err)

	// raw[7] is the number-of-base-commit-graphs byte.
	raw[7] = 1

	idx, err := openIndexBytes(raw)
	s.Require().NoError(err)
	defer idx.Close()
	s.Equal(uint32(1), idx.MaximumNumberOfHashes())
}

// TestEncodeDecodeSHA256 round-trips a SHA-256 commit-graph through the
// encoder and reader, exercising the hash-version header byte, the
// 32-byte OID widths in every chunk, and parent-hash resolution.
func (s *CommitgraphSuite) TestEncodeDecodeSHA256() {
	hashA := plumbing.NewHash(strings.Repeat("a", 64))
	treeA := plumbing.NewHash(strings.Repeat("c", 64))
	hashB := plumbing.NewHash(strings.Repeat("b", 64))
	treeB := plumbing.NewHash(strings.Repeat("d", 64))
	s.Require().Equal(32, hashA.Size(), "test hashes must be SHA-256 sized")

	mem := commitgraph.NewMemoryIndex()
	mem.Add(hashA, &commitgraph.CommitData{
		TreeHash:   treeA,
		Generation: 1,
		When:       time.Unix(1, 0),
	})
	mem.Add(hashB, &commitgraph.CommitData{
		TreeHash:     treeB,
		ParentHashes: []plumbing.Hash{hashA},
		Generation:   2,
		When:         time.Unix(2, 0),
	})

	var buf bytes.Buffer
	s.Require().NoError(commitgraph.NewEncoder(&buf, commitgraph.WithObjectFormat(formatcfg.SHA256)).Encode(mem))
	raw := buf.Bytes()
	s.Equal(byte(2), raw[5], "header hash version byte should be 2 (SHA-256)")

	idx, err := openIndexBytes(raw, commitgraph.WithObjectFormat(formatcfg.SHA256))
	s.Require().NoError(err)
	defer idx.Close()

	bIdx, err := idx.GetIndexByHash(hashB)
	s.Require().NoError(err)

	data, err := idx.GetCommitDataByIndex(bIdx)
	s.Require().NoError(err)
	s.Equal(32, data.TreeHash.Size())
	s.Equal(treeB.String(), data.TreeHash.String())
	s.Require().Len(data.ParentHashes, 1)
	s.Equal(hashA.String(), data.ParentHashes[0].String())

	for _, h := range idx.Hashes() {
		s.Equal(32, h.Size(), "Hashes() must return SHA-256 sized OIDs")
	}
}
