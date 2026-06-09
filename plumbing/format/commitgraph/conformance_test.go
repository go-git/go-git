package commitgraph_test

import (
	encbin "encoding/binary"

	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
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
