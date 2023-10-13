package v2

import (
	"io"
	"math"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

// CommitData is a reduced representation of Commit as presented in the commit graph
// file. It is merely useful as an optimization for walking the commit graphs.
type CommitData struct {
	// TreeHash is the hash of the root tree of the commit.
	TreeHash plumbing.Hash
	// ParentIndexes are the indexes of the parent commits of the commit.
	ParentIndexes []uint32
	// ParentHashes are the hashes of the parent commits of the commit.
	ParentHashes []plumbing.Hash
	// Generation number is the pre-computed generation in the commit graph
	// or zero if not available.
	Generation uint64
	// GenerationV2 stores the corrected commit date for the commits
	// It combines the contents of the GDA2 and GDO2 sections of the commit-graph
	// with the commit time portion of the CDAT section.
	GenerationV2 uint64
	// When is the timestamp of the commit.
	When time.Time
}

// GenerationV2Data returns the corrected commit date for the commits
func (c *CommitData) GenerationV2Data() uint64 {
	if c.GenerationV2 == 0 || c.GenerationV2 == math.MaxUint64 {
		return 0
	}
	return c.GenerationV2 - uint64(c.When.Unix())
}

// Index represents a representation of commit graph that allows indexed
// access to the nodes using commit object hash
type Index interface {
	// GetIndexByHash gets the index in the commit graph from commit hash, if available
	GetIndexByHash(h plumbing.Hash) (uint32, error)
	// GetHashByIndex gets the hash given an index in the commit graph
	GetHashByIndex(i uint32) (plumbing.Hash, error)
	// GetNodeByIndex gets the commit node from the commit graph using index
	// obtained from child node, if available
	GetCommitDataByIndex(i uint32) (*CommitData, error)
	// Hashes returns all the hashes that are available in the index
	Hashes() []plumbing.Hash
	// HasGenerationV2 returns true if the commit graph has the corrected commit date data
	HasGenerationV2() bool
	// MaximumNumberOfHashes returns the maximum number of hashes within the index
	MaximumNumberOfHashes() uint32

	io.Closer
}
