package commitgraph

import (
	"crypto"
	"io"
	"math"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

// Encoder writes MemoryIndex structs to an output stream.
type Encoder struct {
	io.Writer
	hash hash.Hash
}

// NewEncoder returns a new stream encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	// TODO: Support passing an ObjectFormat (sha256)
	h := hash.New(crypto.SHA1)
	mw := io.MultiWriter(w, h)
	return &Encoder{mw, h}
}

// Encode writes an index into the commit-graph file
func (e *Encoder) Encode(idx Index) error {
	// Get all the hashes in the input index
	hashes := idx.Hashes()

	// Sort the input and prepare helper structures we'll need for encoding
	hashToIndex, fanout, extraEdgesCount, generationV2OverflowCount := e.prepare(idx, hashes)

	chunkSignatures := [][]byte{OIDFanoutChunk.Signature(), OIDLookupChunk.Signature(), CommitDataChunk.Signature()}
	//nolint:gosec // G115: len() and hash.Size() are always small positive values
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

	extraEdges, generationV2Data, err := e.encodeCommitData(hashes, hashToIndex, idx)
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

func (e *Encoder) prepare(idx Index, hashes []plumbing.Hash) (hashToIndex map[plumbing.Hash]uint32, fanout []uint32, extraEdgesCount, generationV2OverflowCount uint32) {
	// Sort the hashes and build our index
	plumbing.HashesSort(hashes)
	hashToIndex = make(map[plumbing.Hash]uint32)
	fanout = make([]uint32, lenFanout)
	for i, hash := range hashes {
		hashToIndex[hash] = uint32(i) //nolint:gosec // G115: i is loop index bounded by hashes count
		fanout[hash.Bytes()[0]]++
	}

	// Convert the fanout to cumulative values
	for i := 1; i < lenFanout; i++ {
		fanout[i] += fanout[i-1]
	}

	hasGenerationV2 := idx.HasGenerationV2()

	// Find out if we will need extra edge table
	for i := range len(hashes) {
		v, _ := idx.GetCommitDataByIndex(uint32(i)) //nolint:gosec // G115: i is loop index
		if len(v.ParentHashes) > 2 {
			extraEdgesCount += uint32(len(v.ParentHashes) - 1) //nolint:gosec // G115: parent count is small
		}
		if hasGenerationV2 && v.GenerationV2Data() > math.MaxUint32 {
			generationV2OverflowCount++
		}
	}

	return hashToIndex, fanout, extraEdgesCount, generationV2OverflowCount
}

func (e *Encoder) encodeFileHeader(chunkCount int) (err error) {
	if _, err = e.Write(commitFileSignature); err == nil {
		version := byte(1)
		if crypto.Hash(e.hash.Size()) == crypto.Hash(crypto.SHA256.Size()) { //nolint:gosec // G115: hash.Size() is always small positive
			version = byte(2)
		}
		_, err = e.Write([]byte{1, version, byte(chunkCount), 0})
	}
	return err
}

func (e *Encoder) encodeChunkHeaders(chunkSignatures [][]byte, chunkSizes []uint64) (err error) {
	// 8 bytes of file header, 12 bytes for each chunk header and 12 byte for terminator
	offset := uint64(szSignature + szHeader + (len(chunkSignatures)+1)*(szChunkSig+szUint64)) //nolint:gosec // G115: small constants
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

func (e *Encoder) encodeCommitData(hashes []plumbing.Hash, hashToIndex map[plumbing.Hash]uint32, idx Index) (extraEdges []uint32, generationV2Data []uint64, err error) {
	if idx.HasGenerationV2() {
		generationV2Data = make([]uint64, 0, len(hashes))
	}
	for _, hash := range hashes {
		origIndex, _ := idx.GetIndexByHash(hash)
		commitData, _ := idx.GetCommitDataByIndex(origIndex)
		if _, err = e.Write(commitData.TreeHash.Bytes()); err != nil {
			return extraEdges, generationV2Data, err
		}

		var parent1, parent2 uint32
		switch len(commitData.ParentHashes) {
		case 0:
			parent1 = parentNone
			parent2 = parentNone
		case 1:
			parent1 = hashToIndex[commitData.ParentHashes[0]]
			parent2 = parentNone
		case 2:
			parent1 = hashToIndex[commitData.ParentHashes[0]]
			parent2 = hashToIndex[commitData.ParentHashes[1]]
		default:
			parent1 = hashToIndex[commitData.ParentHashes[0]]
			parent2 = uint32(len(extraEdges)) | parentOctopusUsed //nolint:gosec // G115: extraEdges count is bounded
			for _, parentHash := range commitData.ParentHashes[1:] {
				extraEdges = append(extraEdges, hashToIndex[parentHash])
			}
			extraEdges[len(extraEdges)-1] |= parentLast
		}

		if err = binary.WriteUint32(e, parent1); err == nil {
			err = binary.WriteUint32(e, parent2)
		}
		if err != nil {
			return extraEdges, generationV2Data, err
		}

		unixTime := uint64(commitData.When.Unix()) //nolint:gosec // G115: Unix timestamp is always positive for valid commits
		unixTime |= uint64(commitData.Generation) << 34
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
			if err = binary.WriteUint32(e, uint32(head)|0x80000000); err != nil { //nolint:gosec // G115: head is bounded
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
