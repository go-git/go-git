package commitgraph

import (
	"crypto"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

// Encoder writes MemoryIndex structs to an output stream.
type Encoder struct {
	io.Writer
	hash      hash.Hash
	optionErr error
}

// NewEncoder returns a new stream encoder that writes to w. The object
// format defaults to SHA-1; use WithObjectFormat for SHA-256. Invalid options
// are reported by Encode before writing any bytes.
func NewEncoder(w io.Writer, opts ...Option) *Encoder {
	o, err := readOptions(opts)
	h := hash.New(o.hashAlgorithm())
	return &Encoder{Writer: io.MultiWriter(w, h), hash: h, optionErr: err}
}

// Encode writes an index into the commit-graph file. It returns
// formatcfg.ErrInvalidObjectFormat if the Encoder was created with an invalid
// object format, and ErrObjectFormatMismatch if a commit, tree or parent ID
// has another width than that format; neither writes any bytes.
func (e *Encoder) Encode(idx Index) error {
	if e.optionErr != nil {
		return e.optionErr
	}
	e.hash.Reset()
	// Get all the hashes in the input index
	hashes := idx.Hashes()

	// Sort the input and prepare helper structures we'll need for encoding
	hashToIndex, positions, fanout, extraEdgesCount, generationV2OverflowCount, err := e.prepare(idx, hashes)
	if err != nil {
		return err
	}

	chunkSignatures := [][]byte{OIDFanoutChunk.Signature(), OIDLookupChunk.Signature(), CommitDataChunk.Signature()}
	chunkSizes := []uint64{szUint32 * lenFanout, uint64(len(hashes) * e.hash.Size()), uint64(len(hashes) * (e.hash.Size() + szCommitData))}
	if extraEdgesCount > 0 {
		chunkSignatures = append(chunkSignatures, ExtraEdgeListChunk.Signature())
		chunkSizes = append(chunkSizes, uint64(extraEdgesCount)*szUint32)
	}
	if idx.HasGenerationV2() {
		chunkSignatures = append(chunkSignatures, GenerationDataChunk.Signature())
		chunkSizes = append(chunkSizes, uint64(len(hashes))*szUint32)
		if generationV2OverflowCount > 0 {
			chunkSignatures = append(chunkSignatures, GenerationDataOverflowChunk.Signature())
			chunkSizes = append(chunkSizes, uint64(generationV2OverflowCount)*szUint64)
		}
	}

	if err := e.encodeFileHeader(len(chunkSignatures)); err != nil {
		return err
	}
	if err := e.encodeChunkHeaders(chunkSignatures, chunkSizes); err != nil {
		return err
	}
	if err := e.encodeFanout(fanout); err != nil {
		return err
	}
	if err := e.encodeOidLookup(hashes); err != nil {
		return err
	}

	extraEdges, generationV2Data, err := e.encodeCommitData(hashes, positions, hashToIndex, idx)
	if err != nil {
		return err
	}
	if err = e.encodeExtraEdges(extraEdges); err != nil {
		return err
	}
	if idx.HasGenerationV2() {
		overflows, err := e.encodeGenerationV2Data(generationV2Data)
		if err != nil {
			return err
		}
		if err = e.encodeGenerationV2Overflow(overflows); err != nil {
			return err
		}
	}

	return e.encodeChecksum()
}

// lookupParentIndex resolves a parent hash to its position in the file being
// encoded. A bare map read yields index 0 for an absent hash, which would
// silently record an arbitrary commit as the parent, so report it instead.
func lookupParentIndex(hashToIndex map[plumbing.Hash]uint32, h plumbing.Hash) (uint32, error) {
	i, ok := hashToIndex[h]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrParentNotInIndex, h)
	}

	return i, nil
}

// prepare sorts hashes and indexes them. positions[i] is the position of
// hashes[i] in idx, so encodeCommitData does not have to look it up again.
func (e *Encoder) prepare(idx Index, hashes []plumbing.Hash) (hashToIndex map[plumbing.Hash]uint32, positions, fanout []uint32, extraEdgesCount, generationV2OverflowCount uint32, err error) {
	// Sort the hashes and build our index
	plumbing.HashesSort(hashes)
	hashToIndex = make(map[plumbing.Hash]uint32, len(hashes))
	fanout = make([]uint32, lenFanout)
	for i, hash := range hashes {
		if hash.Size() != e.hash.Size() {
			return nil, nil, nil, 0, 0, e.widthMismatch("commit", hash)
		}
		hashToIndex[hash] = uint32(i)
		fanout[hash.Bytes()[0]]++
	}

	// Convert the fanout to cumulative values
	for i := 1; i < lenFanout; i++ {
		fanout[i] += fanout[i-1]
	}

	hasGenerationV2 := idx.HasGenerationV2()

	// Find out if we will need extra edge table. An index that cannot satisfy
	// the lookup returns a nil CommitData, so the error has to be checked
	// before v is dereferenced.
	positions = make([]uint32, len(hashes))
	for i, h := range hashes {
		originalIndex, err := idx.GetIndexByHash(h)
		if err != nil {
			return nil, nil, nil, 0, 0, err
		}
		positions[i] = originalIndex
		v, err := idx.GetCommitDataByIndex(originalIndex)
		if err != nil {
			return nil, nil, nil, 0, 0, err
		}
		if v.TreeHash.Size() != e.hash.Size() {
			return nil, nil, nil, 0, 0, e.widthMismatch("tree", v.TreeHash)
		}
		for _, parent := range v.ParentHashes {
			if parent.Size() != e.hash.Size() {
				return nil, nil, nil, 0, 0, e.widthMismatch("parent", parent)
			}
		}
		if len(v.ParentHashes) > 2 {
			extraEdgesCount += uint32(len(v.ParentHashes) - 1)
		}
		if hasGenerationV2 && v.GenerationV2Data() >= 0x80000000 {
			generationV2OverflowCount++
		}
	}

	return hashToIndex, positions, fanout, extraEdgesCount, generationV2OverflowCount, nil
}

func (e *Encoder) widthMismatch(kind string, h plumbing.Hash) error {
	return fmt.Errorf("%w: %d-byte %s ID %s, want %d bytes", ErrObjectFormatMismatch, h.Size(), kind, h, e.hash.Size())
}

func (e *Encoder) encodeFileHeader(chunkCount int) (err error) {
	if chunkCount > 255 {
		return ErrTooManyChunks
	}
	if _, err = e.Write(commitFileSignature); err == nil {
		version := byte(1)
		if e.hash.Size() == crypto.SHA256.Size() {
			version = byte(2)
		}
		_, err = e.Write([]byte{1, version, byte(chunkCount), 0})
	}
	return err
}

func (e *Encoder) encodeChunkHeaders(chunkSignatures [][]byte, chunkSizes []uint64) (err error) {
	// 8 bytes of file header, 12 bytes for each chunk header and 12 byte for terminator
	offset := uint64(szSignature + szHeader + (len(chunkSignatures)+1)*(szChunkSig+szUint64))
	for i, signature := range chunkSignatures {
		if _, err = e.Write(signature); err == nil {
			err = binary.WriteUint64(e, offset)
		}
		if err != nil {
			return err
		}
		offset += chunkSizes[i]
	}
	if _, err = e.Write(ZeroChunk.Signature()); err == nil {
		err = binary.WriteUint64(e, offset)
	}
	return err
}

func (e *Encoder) encodeFanout(fanout []uint32) (err error) {
	for i := 0; i <= 0xff; i++ {
		if err = binary.WriteUint32(e, fanout[i]); err != nil {
			return err
		}
	}
	return err
}

func (e *Encoder) encodeOidLookup(hashes []plumbing.Hash) (err error) {
	for _, hash := range hashes {
		if _, err = e.Write(hash.Bytes()); err != nil {
			return err
		}
	}
	return err
}

func (e *Encoder) encodeCommitData(hashes []plumbing.Hash, positions []uint32, hashToIndex map[plumbing.Hash]uint32, idx Index) (extraEdges []uint32, generationV2Data []uint64, err error) {
	if idx.HasGenerationV2() {
		generationV2Data = make([]uint64, 0, len(hashes))
	}
	for i := range hashes {
		// The lookup can fail, and commitData is nil when it does.
		commitData, err := idx.GetCommitDataByIndex(positions[i])
		if err != nil {
			return extraEdges, generationV2Data, err
		}
		if _, err = e.Write(commitData.TreeHash.Bytes()); err != nil {
			return extraEdges, generationV2Data, err
		}

		var parent1, parent2 uint32
		switch len(commitData.ParentHashes) {
		case 0:
			parent1 = parentNone
			parent2 = parentNone
		case 1:
			if parent1, err = lookupParentIndex(hashToIndex, commitData.ParentHashes[0]); err != nil {
				return extraEdges, generationV2Data, err
			}
			parent2 = parentNone
		case 2:
			if parent1, err = lookupParentIndex(hashToIndex, commitData.ParentHashes[0]); err != nil {
				return extraEdges, generationV2Data, err
			}
			if parent2, err = lookupParentIndex(hashToIndex, commitData.ParentHashes[1]); err != nil {
				return extraEdges, generationV2Data, err
			}
		default:
			if parent1, err = lookupParentIndex(hashToIndex, commitData.ParentHashes[0]); err != nil {
				return extraEdges, generationV2Data, err
			}
			parent2 = uint32(len(extraEdges)) | parentOctopusUsed
			for _, parentHash := range commitData.ParentHashes[1:] {
				extraEdge, err := lookupParentIndex(hashToIndex, parentHash)
				if err != nil {
					return extraEdges, generationV2Data, err
				}
				extraEdges = append(extraEdges, extraEdge)
			}
			extraEdges[len(extraEdges)-1] |= parentLast
		}

		if err = binary.WriteUint32(e, parent1); err == nil {
			err = binary.WriteUint32(e, parent2)
		}
		if err != nil {
			return extraEdges, generationV2Data, err
		}

		// The date keeps its low 34 bits, as in Git's
		// [write_graph_chunk_data], and the generation is capped at
		// GENERATION_NUMBER_V1_MAX, as in [compute_generation_from_max], so
		// neither field can spill into the other.
		//
		// [write_graph_chunk_data]: https://github.com/git/git/blob/v2.55.0/commit-graph.c#L1306-L1313
		// [compute_generation_from_max]: https://github.com/git/git/blob/v2.55.0/commit-graph.c#L1630-L1645
		unixTime := uint64(commitData.When.Unix()) & commitTimeMask
		unixTime |= min(commitData.Generation, 0x3FFFFFFF) << 34
		if err = binary.WriteUint64(e, unixTime); err != nil {
			return extraEdges, generationV2Data, err
		}
		if generationV2Data != nil {
			generationV2Data = append(generationV2Data, commitData.GenerationV2Data())
		}
	}
	return extraEdges, generationV2Data, err
}

func (e *Encoder) encodeExtraEdges(extraEdges []uint32) (err error) {
	for _, parent := range extraEdges {
		if err = binary.WriteUint32(e, parent); err != nil {
			return err
		}
	}
	return err
}

func (e *Encoder) encodeGenerationV2Data(generationV2Data []uint64) (overflows []uint64, err error) {
	head := 0
	for _, data := range generationV2Data {
		if data >= 0x80000000 {
			// overflow
			if err = binary.WriteUint32(e, uint32(head)|0x80000000); err != nil {
				return nil, err
			}
			generationV2Data[head] = data
			head++
			continue
		}
		if err = binary.WriteUint32(e, uint32(data)); err != nil {
			return nil, err
		}
	}

	return generationV2Data[:head], nil
}

func (e *Encoder) encodeGenerationV2Overflow(overflows []uint64) (err error) {
	for _, overflow := range overflows {
		if err = binary.WriteUint64(e, overflow); err != nil {
			return err
		}
	}
	return err
}

func (e *Encoder) encodeChecksum() error {
	_, err := e.Write(e.hash.Sum(nil)[:e.hash.Size()])
	return err
}
