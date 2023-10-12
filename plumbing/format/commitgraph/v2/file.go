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
	commitDataSize = 16
)

type fileIndex struct {
	reader  ReaderAtCloser
	fanout  [256]uint32
	offsets [9]int64
	parent  Index
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
	signature := make([]byte, 4)
	if _, err := fi.reader.ReadAt(signature, 0); err != nil {
		return err
	}
	if !bytes.Equal(signature, commitFileSignature) {
		return ErrMalformedCommitGraphFile
	}

	// Read and verify the file header
	header := make([]byte, 4)
	if _, err := fi.reader.ReadAt(header, 4); err != nil {
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
	chunkID := make([]byte, 4)
	for i := 0; ; i++ {
		chunkHeader := io.NewSectionReader(fi.reader, 8+(int64(i)*12), 12)
		if _, err := io.ReadAtLeast(chunkHeader, chunkID, 4); err != nil {
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
	fanoutReader := io.NewSectionReader(fi.reader, fi.offsets[OIDFanoutChunk], 256*4)
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
			return mid, nil
		} else {
			low = mid + 1
		}
	}

	if fi.parent != nil {
		idx, err := fi.parent.GetIndexByHash(h)
		if err != nil {
			return 0, err
		}
		return idx + fi.fanout[0xff], nil
	}

	return 0, plumbing.ErrObjectNotFound
}

// GetCommitDataByIndex returns the commit data for the given index in the commit-graph.
func (fi *fileIndex) GetCommitDataByIndex(idx uint32) (*CommitData, error) {
	if idx >= fi.fanout[0xff] {
		if fi.parent != nil {
			data, err := fi.parent.GetCommitDataByIndex(idx - fi.fanout[0xff])
			if err != nil {
				return nil, err
			}
			for i := range data.ParentIndexes {
				data.ParentIndexes[i] += fi.fanout[0xff]
			}
			return data, nil
		}

		return nil, plumbing.ErrObjectNotFound
	}

	offset := fi.offsets[CommitDataChunk] + int64(idx)*(hash.Size+commitDataSize)
	commitDataReader := io.NewSectionReader(fi.reader, offset, hash.Size+commitDataSize)

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
		// Octopus merge
		parentIndexes = []uint32{parent1 & parentOctopusMask}
		offset := fi.offsets[ExtraEdgeListChunk] + 4*int64(parent2&parentOctopusMask)
		buf := make([]byte, 4)
		for {
			_, err := fi.reader.ReadAt(buf, offset)
			if err != nil {
				return nil, err
			}

			parent := encbin.BigEndian.Uint32(buf)
			offset += 4
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

	return &CommitData{
		TreeHash:      treeHash,
		ParentIndexes: parentIndexes,
		ParentHashes:  parentHashes,
		Generation:    genAndTime >> 34,
		When:          time.Unix(int64(genAndTime&0x3FFFFFFFF), 0),
	}, nil
}

// GetHashByIndex looks up the hash for the given index in the commit-graph.
func (fi *fileIndex) GetHashByIndex(idx uint32) (found plumbing.Hash, err error) {
	if idx >= fi.fanout[0xff] {
		if fi.parent != nil {
			return fi.parent.GetHashByIndex(idx - fi.fanout[0xff])
		}
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
		if idx >= fi.fanout[0xff] {
			if fi.parent != nil {
				hash, err := fi.parent.GetHashByIndex(idx - fi.fanout[0xff])
				if err != nil {
					return nil, err
				}
				hashes[i] = hash
				continue
			}

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
	hashes := make([]plumbing.Hash, fi.fanout[0xff])
	for i := uint32(0); i < fi.fanout[0xff]; i++ {
		offset := fi.offsets[OIDLookupChunk] + int64(i)*hash.Size
		if n, err := fi.reader.ReadAt(hashes[i][:], offset); err != nil || n < hash.Size {
			return nil
		}
	}
	if fi.parent != nil {
		parentHashes := fi.parent.Hashes()
		hashes = append(hashes, parentHashes...)
	}
	return hashes
}
