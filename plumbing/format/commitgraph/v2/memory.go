package v2

import (
	"math"

	"github.com/go-git/go-git/v5/plumbing"
)

// MemoryIndex provides a way to build the commit-graph in memory
// for later encoding to file.
type MemoryIndex struct {
	commitData      []commitData
	indexMap        map[plumbing.Hash]uint32
	hasGenerationV2 bool
}

type commitData struct {
	Hash plumbing.Hash
	*CommitData
}

// NewMemoryIndex creates in-memory commit graph representation
func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{
		indexMap:        make(map[plumbing.Hash]uint32),
		hasGenerationV2: true,
	}
}

// GetIndexByHash gets the index in the commit graph from commit hash, if available
func (mi *MemoryIndex) GetIndexByHash(h plumbing.Hash) (uint32, error) {
	i, ok := mi.indexMap[h]
	if ok {
		return i, nil
	}

	return 0, plumbing.ErrObjectNotFound
}

// GetHashByIndex gets the hash given an index in the commit graph
func (mi *MemoryIndex) GetHashByIndex(i uint32) (plumbing.Hash, error) {
	if i >= uint32(len(mi.commitData)) {
		return plumbing.ZeroHash, plumbing.ErrObjectNotFound
	}

	return mi.commitData[i].Hash, nil
}

// GetCommitDataByIndex gets the commit node from the commit graph using index
// obtained from child node, if available
func (mi *MemoryIndex) GetCommitDataByIndex(i uint32) (*CommitData, error) {
	if i >= uint32(len(mi.commitData)) {
		return nil, plumbing.ErrObjectNotFound
	}

	commitData := mi.commitData[i]

	// Map parent hashes to parent indexes
	if commitData.ParentIndexes == nil {
		parentIndexes := make([]uint32, len(commitData.ParentHashes))
		for i, parentHash := range commitData.ParentHashes {
			var err error
			if parentIndexes[i], err = mi.GetIndexByHash(parentHash); err != nil {
				return nil, err
			}
		}
		commitData.ParentIndexes = parentIndexes
	}

	return commitData.CommitData, nil
}

// Hashes returns all the hashes that are available in the index
func (mi *MemoryIndex) Hashes() []plumbing.Hash {
	hashes := make([]plumbing.Hash, 0, len(mi.indexMap))
	for k := range mi.indexMap {
		hashes = append(hashes, k)
	}
	return hashes
}

// Add adds new node to the memory index
func (mi *MemoryIndex) Add(hash plumbing.Hash, data *CommitData) {
	// The parent indexes are calculated lazily in GetNodeByIndex
	// which allows adding nodes out of order as long as all parents
	// are eventually resolved
	data.ParentIndexes = nil
	mi.indexMap[hash] = uint32(len(mi.commitData))
	mi.commitData = append(mi.commitData, commitData{Hash: hash, CommitData: data})
	if data.GenerationV2 == math.MaxUint64 { // if GenerationV2 is not available reset it to zero
		data.GenerationV2 = 0
	}
	mi.hasGenerationV2 = mi.hasGenerationV2 && data.GenerationV2 != 0
}

func (mi *MemoryIndex) HasGenerationV2() bool {
	return mi.hasGenerationV2
}

// Close closes the index
func (mi *MemoryIndex) Close() error {
	return nil
}

func (mi *MemoryIndex) MaximumNumberOfHashes() uint32 {
	return uint32(len(mi.indexMap))
}
