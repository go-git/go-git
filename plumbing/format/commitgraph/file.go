package commitgraph

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

// Deprecated: This package uses the wrong types for Generation and Index in CommitData.
// Use the v2 package instead.

var (
	// ErrUnsupportedVersion is returned by OpenFileIndex when the commit graph
	// file version is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrUnsupportedHash is returned by OpenFileIndex when the commit graph
	// hash function is not supported. Currently only SHA-1 is defined and
	// supported
	ErrUnsupportedHash = errors.New("unsupported hash algorithm")
	// ErrMalformedCommitGraphFile is returned by OpenFileIndex when the commit
	// graph file is corrupted.
	ErrMalformedCommitGraphFile = errors.New("malformed commit graph file")

	commitFileSignature    = []byte{'C', 'G', 'P', 'H'}
	oidFanoutSignature     = []byte{'O', 'I', 'D', 'F'}
	oidLookupSignature     = []byte{'O', 'I', 'D', 'L'}
	commitDataSignature    = []byte{'C', 'D', 'A', 'T'}
	extraEdgeListSignature = []byte{'E', 'D', 'G', 'E'}
	lastSignature          = []byte{0, 0, 0, 0}

	parentNone        = uint32(0x70000000)
	parentOctopusUsed = uint32(0x80000000)
	parentOctopusMask = uint32(0x7fffffff)
	parentLast        = uint32(0x80000000)
)

const commitDataSize = 16

type fileIndex struct {
	reader              io.ReaderAt
	fanout              [256]int
	oidFanoutOffset     int64
	oidLookupOffset     int64
	commitDataOffset    int64
	extraEdgeListOffset int64
}

// OpenFileIndex opens a serialized commit graph file in the format described at
// https://github.com/git/git/blob/master/Documentation/technical/commit-graph-format.txt
//
// Deprecated: This package uses the wrong types for Generation and Index in CommitData.
// Use the v2 package instead.
func OpenFileIndex(reader io.ReaderAt) (Index, error) {
	fi := &fileIndex{reader: reader}

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
	if hash.CryptoType == crypto.SHA1 {
		if header[1] != 1 {
			return ErrUnsupportedVersion
		}
	} else if hash.CryptoType == crypto.SHA256 {
		if header[1] != 2 {
			return ErrUnsupportedVersion
		}
	} else {
		// Unknown hash type
		return ErrUnsupportedVersion
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

		if bytes.Equal(chunkID, oidFanoutSignature) {
			fi.oidFanoutOffset = int64(chunkOffset)
		} else if bytes.Equal(chunkID, oidLookupSignature) {
			fi.oidLookupOffset = int64(chunkOffset)
		} else if bytes.Equal(chunkID, commitDataSignature) {
			fi.commitDataOffset = int64(chunkOffset)
		} else if bytes.Equal(chunkID, extraEdgeListSignature) {
			fi.extraEdgeListOffset = int64(chunkOffset)
		} else if bytes.Equal(chunkID, lastSignature) {
			break
		}
	}

	if fi.oidFanoutOffset <= 0 || fi.oidLookupOffset <= 0 || fi.commitDataOffset <= 0 {
		return ErrMalformedCommitGraphFile
	}

	return nil
}

func (fi *fileIndex) readFanout() error {
	fanoutReader := io.NewSectionReader(fi.reader, fi.oidFanoutOffset, 256*4)
	for i := 0; i < 256; i++ {
		fanoutValue, err := binary.ReadUint32(fanoutReader)
		if err != nil {
			return err
		}
		if fanoutValue > 0x7fffffff {
			return ErrMalformedCommitGraphFile
		}
		fi.fanout[i] = int(fanoutValue)
	}
	return nil
}

func (fi *fileIndex) GetIndexByHash(h plumbing.Hash) (int, error) {
	var oid plumbing.Hash

	// Find the hash in the oid lookup table
	var low int
	if h[0] == 0 {
		low = 0
	} else {
		low = fi.fanout[h[0]-1]
	}
	high := fi.fanout[h[0]]
	for low < high {
		mid := (low + high) >> 1
		offset := fi.oidLookupOffset + int64(mid)*hash.Size
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

	return 0, plumbing.ErrObjectNotFound
}

func (fi *fileIndex) GetCommitDataByIndex(idx int) (*CommitData, error) {
	if idx >= fi.fanout[0xff] {
		return nil, plumbing.ErrObjectNotFound
	}

	offset := fi.commitDataOffset + int64(idx)*(hash.Size+commitDataSize)
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

	var parentIndexes []int
	if parent2&parentOctopusUsed == parentOctopusUsed {
		// Octopus merge
		parentIndexes = []int{int(parent1 & parentOctopusMask)}
		offset := fi.extraEdgeListOffset + 4*int64(parent2&parentOctopusMask)
		buf := make([]byte, 4)
		for {
			_, err := fi.reader.ReadAt(buf, offset)
			if err != nil {
				return nil, err
			}

			parent := encbin.BigEndian.Uint32(buf)
			offset += 4
			parentIndexes = append(parentIndexes, int(parent&parentOctopusMask))
			if parent&parentLast == parentLast {
				break
			}
		}
	} else if parent2 != parentNone {
		parentIndexes = []int{int(parent1 & parentOctopusMask), int(parent2 & parentOctopusMask)}
	} else if parent1 != parentNone {
		parentIndexes = []int{int(parent1 & parentOctopusMask)}
	}

	parentHashes, err := fi.getHashesFromIndexes(parentIndexes)
	if err != nil {
		return nil, err
	}

	return &CommitData{
		TreeHash:      treeHash,
		ParentIndexes: parentIndexes,
		ParentHashes:  parentHashes,
		Generation:    int(genAndTime >> 34),
		When:          time.Unix(int64(genAndTime&0x3FFFFFFFF), 0),
	}, nil
}

func (fi *fileIndex) getHashesFromIndexes(indexes []int) ([]plumbing.Hash, error) {
	hashes := make([]plumbing.Hash, len(indexes))

	for i, idx := range indexes {
		if idx >= fi.fanout[0xff] {
			return nil, ErrMalformedCommitGraphFile
		}

		offset := fi.oidLookupOffset + int64(idx)*hash.Size
		if _, err := fi.reader.ReadAt(hashes[i][:], offset); err != nil {
			return nil, err
		}
	}

	return hashes, nil
}

// Hashes returns all the hashes that are available in the index
func (fi *fileIndex) Hashes() []plumbing.Hash {
	hashes := make([]plumbing.Hash, fi.fanout[0xff])
	for i := 0; i < fi.fanout[0xff]; i++ {
		offset := fi.oidLookupOffset + int64(i)*hash.Size
		if n, err := fi.reader.ReadAt(hashes[i][:], offset); err != nil || n < hash.Size {
			return nil
		}
	}
	return hashes
}
