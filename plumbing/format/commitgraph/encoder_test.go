package commitgraph

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestEncodeFileHeaderRejectsTooManyChunks(t *testing.T) {
	t.Parallel()
	// The on-disk format stores num_chunks as a uint8, so any encoder
	// path that tries to emit more than 255 chunk types would silently
	// truncate. The encoder must reject the configuration at write time.
	e := NewEncoder(&bytes.Buffer{})

	err := e.encodeFileHeader(256)
	assert.ErrorIs(t, err, ErrTooManyChunks)
}

// A commit whose parent was never added to the index must be reported, not
// dereferenced as a nil CommitData. This is the shape a shallow clone's
// boundary commit has, since its ParentHashes still name the unfetched parent.
func TestEncodeUnresolvableParentReturnsError(t *testing.T) {
	t.Parallel()

	mi := NewMemoryIndex()
	mi.Add(plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), &CommitData{
		TreeHash:     plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		ParentHashes: []plumbing.Hash{plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
		When:         time.Unix(1700000000, 0),
		Generation:   1,
		GenerationV2: 1,
	})

	err := NewEncoder(&bytes.Buffer{}).Encode(mi)
	require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}

// A merge commit with one parent in the index and one outside it must be
// reported too, rather than encoding only the resolvable edge.
func TestEncodePartiallyResolvableParentsReturnsError(t *testing.T) {
	t.Parallel()

	tree := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	present := plumbing.NewHash("1111111111111111111111111111111111111111")

	mi := NewMemoryIndex()
	mi.Add(present, &CommitData{
		TreeHash:     tree,
		When:         time.Unix(1700000000, 0),
		Generation:   1,
		GenerationV2: 1,
	})
	mi.Add(plumbing.NewHash("3333333333333333333333333333333333333333"), &CommitData{
		TreeHash: tree,
		ParentHashes: []plumbing.Hash{
			present,
			plumbing.NewHash("2222222222222222222222222222222222222222"),
		},
		When:         time.Unix(1700000100, 0),
		Generation:   2,
		GenerationV2: 2,
	})

	err := NewEncoder(&bytes.Buffer{}).Encode(mi)
	require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}

// danglingEdgeIndex reports a parent that Hashes() does not include, so the
// parent hash cannot be mapped to a position in the file being written. A
// MemoryIndex rejects this earlier, so reaching the encoder's own lookup needs
// a separate Index implementation.
type danglingEdgeIndex struct {
	hash   plumbing.Hash
	parent plumbing.Hash
}

func (i danglingEdgeIndex) GetIndexByHash(h plumbing.Hash) (uint32, error) {
	if h == i.hash {
		return 0, nil
	}

	return 0, plumbing.ErrObjectNotFound
}

func (i danglingEdgeIndex) GetHashByIndex(uint32) (plumbing.Hash, error) { return i.hash, nil }

func (i danglingEdgeIndex) GetCommitDataByIndex(uint32) (*CommitData, error) {
	return &CommitData{
		TreeHash:      plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		ParentIndexes: []uint32{1},
		ParentHashes:  []plumbing.Hash{i.parent},
		When:          time.Unix(1700000000, 0),
	}, nil
}

func (i danglingEdgeIndex) Hashes() []plumbing.Hash       { return []plumbing.Hash{i.hash} }
func (i danglingEdgeIndex) HasGenerationV2() bool         { return false }
func (i danglingEdgeIndex) MaximumNumberOfHashes() uint32 { return 1 }
func (i danglingEdgeIndex) Close() error                  { return nil }

// An unresolvable parent hash must not be written as index 0, which is a real
// but arbitrary commit.
func TestEncodeDanglingEdgeReturnsError(t *testing.T) {
	t.Parallel()

	idx := danglingEdgeIndex{
		hash:   plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		parent: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	err := NewEncoder(&bytes.Buffer{}).Encode(idx)
	require.ErrorIs(t, err, ErrParentNotInIndex)
}
