package commitgraph

import (
	"crypto/sha1"
	"hash"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/utils/binary"
)

// Encoder writes MemoryIndex structs to an output stream.
type Encoder struct {
	io.Writer
	hash hash.Hash
}

// NewEncoder returns a new stream encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	h := sha1.New()
	mw := io.MultiWriter(w, h)
	return &Encoder{mw, h}
}

func (e *Encoder) Encode(idx Index) error {
	var err error

	// Get all the hashes in the memory index
	hashes := idx.Hashes()

	// Sort the hashes and build our index
	plumbing.HashesSort(hashes)
	hashToIndex := make(map[plumbing.Hash]uint32)
	hashFirstToCount := make(map[byte]uint32)
	for i, hash := range hashes {
		hashToIndex[hash] = uint32(i)
		hashFirstToCount[hash[0]]++
	}

	// Find out if we will need large edge table
	largeEdgesCount := 0
	for i := 0; i < len(hashes); i++ {
		v, _ := idx.GetNodeByIndex(i)
		if len(v.ParentHashes) > 2 {
			largeEdgesCount += len(v.ParentHashes) - 2
			break
		}
	}

	chunks := [][]byte{oidFanoutSignature, oidLookupSignature, commitDataSignature}
	chunkSizes := []uint64{4 * 256, uint64(len(hashes)) * 20, uint64(len(hashes)) * 36}
	if largeEdgesCount > 0 {
		chunks = append(chunks, largeEdgeListSignature)
		chunkSizes = append(chunkSizes, uint64(largeEdgesCount)*4)
	}

	// Write header
	if _, err = e.Write(commitFileSignature); err == nil {
		_, err = e.Write([]byte{1, 1, byte(len(chunks)), 0})
	}
	if err != nil {
		return err
	}

	// Write chunk headers
	offset := uint64(8 + len(chunks)*12 + 12)
	for i, signature := range chunks {
		if _, err = e.Write(signature); err == nil {
			err = binary.WriteUint64(e, offset)
		}
		if err != nil {
			return err
		}
		offset += chunkSizes[i]
	}
	if _, err = e.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	// Write fanout
	var cumulative uint32
	for i := 0; i <= 0xff; i++ {
		if err = binary.WriteUint32(e, hashFirstToCount[byte(i)]+cumulative); err != nil {
			return err
		}
		cumulative += hashFirstToCount[byte(i)]
	}

	// Write OID lookup
	for _, hash := range hashes {
		if _, err = e.Write(hash[:]); err != nil {
			return err
		}
	}

	// Write commit data
	var largeEdges []uint32
	for _, hash := range hashes {
		origIndex, _ := idx.GetIndexByHash(hash)
		commitData, _ := idx.GetNodeByIndex(origIndex)
		if _, err := e.Write(commitData.TreeHash[:]); err != nil {
			return err
		}

		var parent1, parent2 uint32
		if len(commitData.ParentHashes) == 0 {
			parent1 = parentNone
			parent2 = parentNone
		} else if len(commitData.ParentHashes) == 1 {
			parent1 = hashToIndex[commitData.ParentHashes[0]]
			parent2 = parentNone
		} else if len(commitData.ParentHashes) == 2 {
			parent1 = hashToIndex[commitData.ParentHashes[0]]
			parent2 = hashToIndex[commitData.ParentHashes[1]]
		} else if len(commitData.ParentHashes) > 2 {
			parent1 = hashToIndex[commitData.ParentHashes[0]]
			parent2 = uint32(len(largeEdges)) | parentOctopusUsed
			for _, parentHash := range commitData.ParentHashes[1:] {
				largeEdges = append(largeEdges, hashToIndex[parentHash])
			}
			largeEdges[len(largeEdges)-1] |= parentLast
		}

		if err = binary.WriteUint32(e, parent1); err == nil {
			err = binary.WriteUint32(e, parent2)
		}
		if err != nil {
			return err
		}

		unixTime := uint64(commitData.When.Unix())
		unixTime |= uint64(commitData.Generation) << 34
		if err = binary.WriteUint64(e, unixTime); err != nil {
			return err
		}
	}

	// Write large edges if necessary
	for _, parent := range largeEdges {
		if err = binary.WriteUint32(e, parent); err != nil {
			return err
		}
	}

	// Write checksum
	if _, err := e.Write(e.hash.Sum(nil)[:20]); err != nil {
		return err
	}

	return nil
}
