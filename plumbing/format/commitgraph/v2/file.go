package v2

import (
	"bytes"
	"crypto"
	encbin "encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/hash"
	"github.com/go-git/go-git/v5/utils/binary"
)

var (
	// ErrUnsupportedVersion is returned by OpenFileIndex when the commit graph
	// file version is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrUnsupportedHash is returned by OpenFileIndex when the commit graph
	// hash function is not supported. Currently only SHA-1 is defined and
	// supported.
	ErrUnsupportedHash = errors.New("unsupported hash algorithm")
	// ErrMalformedCommitGraphFile is returned by OpenFileIndex when the commit
	// graph file is corrupted.
	ErrMalformedCommitGraphFile = errors.New("malformed commit graph file")

	commitFileSignature = []byte{'C', 'G', 'P', 'H'}

	parentNone        = uint32(0x70000000)
	parentOctopusUsed = uint32(0x80000000)
	parentOctopusMask = uint32(0x7fffffff)
	parentLast        = uint32(0x80000000)
)

const (
	szUint32 = 4
	szUint64 = 8

	szSignature  = 4
	szHeader     = 4
	szCommitData = 2*szUint32 + szUint64

	lenFanout = 256
)

type fileIndex struct {
	reader                ReaderAtCloser
	fanout                [lenFanout]uint32
	offsets               [lenChunks]int64
	parent                Index
	hasGenerationV2       bool
	minimumNumberOfHashes uint32
}

// ReaderAtCloser is an interface that combines io.ReaderAt and io.Closer.
type ReaderAtCloser interface {
	io.ReaderAt
	io.Closer
}

// OpenFileIndex opens a serialized commit graph file in the format described at
// https://github.com/git/git/blob/master/Documentation/technical/commit-graph-format.txt
func OpenFileIndex(reader ReaderAtCloser) (Index, error) {
	return OpenFileIndexWithParent(reader, nil)
}

// OpenFileIndexWithParent opens a serialized commit graph file in the format described at
// https://github.com/git/git/blob/master/Documentation/technical/commit-graph-format.txt
func OpenFileIndexWithParent(reader ReaderAtCloser, parent Index) (Index, error) {
	if reader == nil {
		return nil, io.ErrUnexpectedEOF
	}
	fi := &fileIndex{reader: reader, parent: parent}

	if err := fi.verifyFileHeader(); err != nil {
		return nil, err
	}
	if err := fi.readChunkHeaders(); err != nil {
		return nil, err
	}
	if err := fi.readFanout(); err != nil {
		return nil, err
	}

	fi.hasGenerationV2 = fi.offsets[GenerationDataChunk] > 0
	if fi.parent != nil {
		fi.hasGenerationV2 = fi.hasGenerationV2 && fi.parent.HasGenerationV2()
	}

	if fi.parent != nil {
		fi.minimumNumberOfHashes = fi.parent.MaximumNumberOfHashes()
	}

	return fi, nil
}

// Close closes the underlying reader and the parent index if it exists.
func (fi *fileIndex) Close() (err error) {
	if fi.parent != nil {
		defer func() {
			parentErr := fi.parent.Close()
			// only report the error from the parent if there is no error from the reader
			if err == nil {
				err = parentErr
			}
		}()
	}
	err = fi.reader.Close()
	return
}

func (fi *fileIndex) verifyFileHeader() error {
	// Verify file signature
	signature := make([]byte, szSignature)
	if _, err := fi.reader.ReadAt(signature, 0); err != nil {
		return err
	}
	if !bytes.Equal(signature, commitFileSignature) {
		return ErrMalformedCommitGraphFile
	}

	// Read and verify the file header
	header := make([]byte, szHeader)
	if _, err := fi.reader.ReadAt(header, szHeader); err != nil {
		return err
	}
	if header[0] != 1 {
		return ErrUnsupportedVersion
	}
	if !(hash.CryptoType == crypto.SHA1 && header[1] == 1) &&
		!(hash.CryptoType == crypto.SHA256 && header[1] == 2) {
		// Unknown hash type / unsupported hash type
		return ErrUnsupportedHash
	}

	return nil
}

func (fi *fileIndex) readChunkHeaders() error {
	// The chunk table is a list of 4-byte chunk signatures and uint64 offsets into the file
	chunkID := make([]byte, szChunkSig)
	for i := 0; ; i++ {
		chunkHeader := io.NewSectionReader(fi.reader, szSignature+szHeader+(int64(i)*(szChunkSig+szUint64)), szChunkSig+szUint64)
		if _, err := io.ReadAtLeast(chunkHeader, chunkID, szChunkSig); err != nil {
			return err
		}
		chunkOffset, err := binary.ReadUint64(chunkHeader)
		if err != nil {
			return err
		}

		chunkType, ok := ChunkTypeFromBytes(chunkID)
		if !ok {
			continue
		}
		if chunkType == ZeroChunk || int(chunkType) >= len(fi.offsets) {
			break
		}
		fi.offsets[chunkType] = int64(chunkOffset)
	}

	if fi.offsets[OIDFanoutChunk] <= 0 || fi.offsets[OIDLookupChunk] <= 0 || fi.offsets[CommitDataChunk] <= 0 {
		return ErrMalformedCommitGraphFile
	}

	return nil
}

func (fi *fileIndex) readFanout() error {
	// The Fanout table is a 256 entry table of the number (as uint32) of OIDs with first byte at most i.
	// Thus F[255] stores the total number of commits (N)
	fanoutReader := io.NewSectionReader(fi.reader, fi.offsets[OIDFanoutChunk], lenFanout*szUint32)
	for i := 0; i < 256; i++ {
		fanoutValue, err := binary.ReadUint32(fanoutReader)
		if err != nil {
			return err
		}
		if fanoutValue > 0x7fffffff {
			return ErrMalformedCommitGraphFile
		}
		fi.fanout[i] = fanoutValue
	}
	return nil
}

// GetIndexByHash looks up the provided hash in the commit-graph fanout and returns the index of the commit data for the given hash.
func (fi *fileIndex) GetIndexByHash(h plumbing.Hash) (uint32, error) {
	var oid plumbing.Hash

	// Find the hash in the oid lookup table
	var low uint32
	if h[0] == 0 {
		low = 0
	} else {
		low = fi.fanout[h[0]-1]
	}
	high := fi.fanout[h[0]]
	for low < high {
		mid := (low + high) >> 1
		offset := fi.offsets[OIDLookupChunk] + int64(mid)*hash.Size
		if _, err := fi.reader.ReadAt(oid[:], offset); err != nil {
			return 0, err
		}
		cmp := bytes.Compare(h[:], oid[:])
		if cmp < 0 {
			high = mid
		} else if cmp == 0 {
			return mid + fi.minimumNumberOfHashes, nil
		} else {
			low = mid + 1
		}
	}

	if fi.parent != nil {
		idx, err := fi.parent.GetIndexByHash(h)
		if err != nil {
			return 0, err
		}
		return idx, nil
	}

	return 0, plumbing.ErrObjectNotFound
}

// GetCommitDataByIndex returns the commit data for the given index in the commit-graph.
func (fi *fileIndex) GetCommitDataByIndex(idx uint32) (*CommitData, error) {
	if idx < fi.minimumNumberOfHashes {
		if fi.parent != nil {
			data, err := fi.parent.GetCommitDataByIndex(idx)
			if err != nil {
				return nil, err
			}
			return data, nil
		}

		return nil, plumbing.ErrObjectNotFound
	}
	idx -= fi.minimumNumberOfHashes
	if idx >= fi.fanout[0xff] {
		return nil, plumbing.ErrObjectNotFound
	}

	offset := fi.offsets[CommitDataChunk] + int64(idx)*(hash.Size+szCommitData)
	commitDataReader := io.NewSectionReader(fi.reader, offset, hash.Size+szCommitData)

	treeHash, err := binary.ReadHash(commitDataReader)
	if err != nil {
		return nil, err
	}
	parent1, err := binary.ReadUint32(commitDataReader)
	if err != nil {
		return nil, err
	}
	parent2, err := binary.ReadUint32(commitDataReader)
	if err != nil {
		return nil, err
	}
	genAndTime, err := binary.ReadUint64(commitDataReader)
	if err != nil {
		return nil, err
	}

	var parentIndexes []uint32
	if parent2&parentOctopusUsed == parentOctopusUsed {
		// Octopus merge - Look-up the extra parents from the extra edge list
		// The extra edge list is a list of uint32s, each of which is an index into the Commit Data table, terminated by a index with the most significant bit on.
		parentIndexes = []uint32{parent1 & parentOctopusMask}
		offset := fi.offsets[ExtraEdgeListChunk] + szUint32*int64(parent2&parentOctopusMask)
		buf := make([]byte, szUint32)
		for {
			_, err := fi.reader.ReadAt(buf, offset)
			if err != nil {
				return nil, err
			}

			parent := encbin.BigEndian.Uint32(buf)
			offset += szUint32
			parentIndexes = append(parentIndexes, parent&parentOctopusMask)
			if parent&parentLast == parentLast {
				break
			}
		}
	} else if parent2 != parentNone {
		parentIndexes = []uint32{parent1 & parentOctopusMask, parent2 & parentOctopusMask}
	} else if parent1 != parentNone {
		parentIndexes = []uint32{parent1 & parentOctopusMask}
	}

	parentHashes, err := fi.getHashesFromIndexes(parentIndexes)
	if err != nil {
		return nil, err
	}

	generationV2 := uint64(0)

	if fi.hasGenerationV2 {
		// set the GenerationV2 result to the commit time
		generationV2 = uint64(genAndTime & 0x3FFFFFFFF)

		// Next read the generation (offset) data from the generation data chunk
		offset := fi.offsets[GenerationDataChunk] + int64(idx)*szUint32
		buf := make([]byte, szUint32)
		if _, err := fi.reader.ReadAt(buf, offset); err != nil {
			return nil, err
		}
		genV2Data := encbin.BigEndian.Uint32(buf)

		// check if the data is an overflow that needs to be looked up in the overflow chunk
		if genV2Data&0x80000000 > 0 {
			// Overflow
			offset := fi.offsets[GenerationDataOverflowChunk] + int64(genV2Data&0x7fffffff)*szUint64
			buf := make([]byte, 8)
			if _, err := fi.reader.ReadAt(buf, offset); err != nil {
				return nil, err
			}

			generationV2 += encbin.BigEndian.Uint64(buf)
		} else {
			generationV2 += uint64(genV2Data)
		}
	}

	return &CommitData{
		TreeHash:      treeHash,
		ParentIndexes: parentIndexes,
		ParentHashes:  parentHashes,
		Generation:    genAndTime >> 34,
		GenerationV2:  generationV2,
		When:          time.Unix(int64(genAndTime&0x3FFFFFFFF), 0),
	}, nil
}

// GetHashByIndex looks up the hash for the given index in the commit-graph.
func (fi *fileIndex) GetHashByIndex(idx uint32) (found plumbing.Hash, err error) {
	if idx < fi.minimumNumberOfHashes {
		if fi.parent != nil {
			return fi.parent.GetHashByIndex(idx)
		}
		return found, ErrMalformedCommitGraphFile
	}
	idx -= fi.minimumNumberOfHashes
	if idx >= fi.fanout[0xff] {
		return found, ErrMalformedCommitGraphFile
	}

	offset := fi.offsets[OIDLookupChunk] + int64(idx)*hash.Size
	if _, err := fi.reader.ReadAt(found[:], offset); err != nil {
		return found, err
	}

	return found, nil
}

func (fi *fileIndex) getHashesFromIndexes(indexes []uint32) ([]plumbing.Hash, error) {
	hashes := make([]plumbing.Hash, len(indexes))

	for i, idx := range indexes {
		if idx < fi.minimumNumberOfHashes {
			if fi.parent != nil {
				hash, err := fi.parent.GetHashByIndex(idx)
				if err != nil {
					return nil, err
				}
				hashes[i] = hash
				continue
			}

			return nil, ErrMalformedCommitGraphFile
		}

		idx -= fi.minimumNumberOfHashes
		if idx >= fi.fanout[0xff] {
			return nil, ErrMalformedCommitGraphFile
		}

		offset := fi.offsets[OIDLookupChunk] + int64(idx)*hash.Size
		if _, err := fi.reader.ReadAt(hashes[i][:], offset); err != nil {
			return nil, err
		}
	}

	return hashes, nil
}

// Hashes returns all the hashes that are available in the index.
func (fi *fileIndex) Hashes() []plumbing.Hash {
	hashes := make([]plumbing.Hash, fi.fanout[0xff]+fi.minimumNumberOfHashes)
	for i := uint32(0); i < fi.minimumNumberOfHashes; i++ {
		hash, err := fi.parent.GetHashByIndex(i)
		if err != nil {
			return nil
		}
		hashes[i] = hash
	}

	for i := uint32(0); i < fi.fanout[0xff]; i++ {
		offset := fi.offsets[OIDLookupChunk] + int64(i)*hash.Size
		if n, err := fi.reader.ReadAt(hashes[i+fi.minimumNumberOfHashes][:], offset); err != nil || n < hash.Size {
			return nil
		}
	}
	return hashes
}

func (fi *fileIndex) HasGenerationV2() bool {
	return fi.hasGenerationV2
}

func (fi *fileIndex) MaximumNumberOfHashes() uint32 {
	return fi.minimumNumberOfHashes + fi.fanout[0xff]
}
