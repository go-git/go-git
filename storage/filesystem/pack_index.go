package filesystem

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
)

const (
	idxMagicV2    = 0xff744f63
	idxVersionV2  = 2
	idxHeaderSize = 8 // magic and version
	fanoutSize    = 256 * 4
	fanoutEntries = 256
)

// packIndex is an idx v2 file reader that reads entries on demand
// using ReadAt, without loading the full index into memory.
// Only the fanout table (1 KiB) and computed table offsets are kept in memory.
type packIndex struct {
	file billy.File

	hashSize   int
	fanout     [fanoutEntries]uint32
	numObjects int

	namesOffset    int64
	offset32Offset int64
	offset64Offset int64
	offset64Count  int
}

// openPackIndex opens and validates one idx v2 file, reading only the
// header and fanout table. All subsequent entry reads use ReadAt.
func openPackIndex(f billy.File, hashSize int) (*packIndex, error) {
	idx := &packIndex{
		file:     f,
		hashSize: hashSize,
	}

	// Read header + fanout in a single ReadAt (8 + 1024 = 1032 bytes).
	var buf [idxHeaderSize + fanoutSize]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return nil, fmt.Errorf("pack index: cannot read header: %w", err)
	}

	magic := binary.BigEndian.Uint32(buf[0:4])
	if magic != idxMagicV2 {
		return nil, fmt.Errorf("pack index: invalid magic %#x", magic)
	}

	version := binary.BigEndian.Uint32(buf[4:8])
	if version != idxVersionV2 {
		return nil, fmt.Errorf("pack index: unsupported version %d", version)
	}

	prev := uint32(0)
	for i := range fanoutEntries {
		cur := binary.BigEndian.Uint32(buf[idxHeaderSize+i*4 : idxHeaderSize+i*4+4])
		if cur < prev {
			return nil, fmt.Errorf("pack index: non-monotonic fanout at %d", i)
		}
		idx.fanout[i] = cur
		prev = cur
	}

	idx.numObjects = int(idx.fanout[255])

	// names, CRC32, offset32, offset64
	namesBytes := int64(idx.numObjects) * int64(hashSize)
	crcBytes := int64(idx.numObjects) * 4
	offset32Bytes := int64(idx.numObjects) * 4

	idx.namesOffset = int64(idxHeaderSize + fanoutSize)
	idx.offset32Offset = idx.namesOffset + namesBytes + crcBytes
	idx.offset64Offset = idx.offset32Offset + offset32Bytes

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("pack index: cannot stat: %w", err)
	}
	fileSize := info.Size()
	trailerSize := int64(2 * hashSize)

	offset64Bytes := fileSize - idx.offset64Offset - trailerSize
	if offset64Bytes < 0 || offset64Bytes%8 != 0 {
		return nil, fmt.Errorf("pack index: malformed 64-bit offset table")
	}
	idx.offset64Count = int(offset64Bytes / 8)

	return idx, nil
}

// FindOffset binary-searches for the given hash and returns its pack offset.
func (idx *packIndex) FindOffset(h plumbing.Hash) (int64, error) {
	hBytes := h.Bytes()
	if len(hBytes) < 1 {
		return 0, plumbing.ErrObjectNotFound
	}

	first := int(hBytes[0])

	lo := 0
	if first > 0 {
		lo = int(idx.fanout[first-1])
	}
	hi := int(idx.fanout[first])

	if lo > hi || hi > idx.numObjects {
		return 0, fmt.Errorf("pack index: invalid fanout bounds")
	}

	nameBuf := make([]byte, idx.hashSize)
	for lo < hi {
		mid := lo + (hi-lo)/2

		nameOff := idx.namesOffset + int64(mid)*int64(idx.hashSize)
		if _, err := idx.file.ReadAt(nameBuf, nameOff); err != nil {
			return 0, fmt.Errorf("pack index: read name: %w", err)
		}

		cmp := bytes.Compare(nameBuf, hBytes)
		if cmp == 0 {
			return idx.offsetAt(mid)
		}
		if cmp < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	return 0, plumbing.ErrObjectNotFound
}

// offsetAt resolves the pack offset for one object at the given index position.
func (idx *packIndex) offsetAt(objectIndex int) (int64, error) {
	if objectIndex < 0 || objectIndex >= idx.numObjects {
		return 0, fmt.Errorf("pack index: object index out of bounds")
	}

	var buf [4]byte
	off := idx.offset32Offset + int64(objectIndex)*4
	if _, err := idx.file.ReadAt(buf[:], off); err != nil {
		return 0, fmt.Errorf("pack index: read 32-bit offset: %w", err)
	}

	word := binary.BigEndian.Uint32(buf[:])
	if word&0x80000000 == 0 {
		return int64(word), nil
	}

	pos := int(word & 0x7fffffff)
	if pos < 0 || pos >= idx.offset64Count {
		return 0, fmt.Errorf("pack index: invalid 64-bit offset position")
	}

	var buf8 [8]byte
	off64 := idx.offset64Offset + int64(pos)*8
	if _, err := idx.file.ReadAt(buf8[:], off64); err != nil {
		return 0, fmt.Errorf("pack index: read 64-bit offset: %w", err)
	}

	return int64(binary.BigEndian.Uint64(buf8[:])), nil
}

// Count returns the total number of objects in this index.
func (idx *packIndex) Count() int {
	return idx.numObjects
}

// EntryAt reads one index entry at the given position
// Returns the hash, pack offset, and any error.
func (idx *packIndex) EntryAt(i int) (plumbing.Hash, int64, error) {
	var h plumbing.Hash
	if i < 0 || i >= idx.numObjects {
		return h, 0, io.EOF
	}

	// Hash
	h.ResetBySize(idx.hashSize)
	hashBuf := make([]byte, idx.hashSize)
	nameOff := idx.namesOffset + int64(i)*int64(idx.hashSize)
	if _, err := idx.file.ReadAt(hashBuf, nameOff); err != nil {
		return h, 0, fmt.Errorf("pack index: read entry name: %w", err)
	}
	_, _ = h.Write(hashBuf)

	// Offset
	offset, err := idx.offsetAt(i)
	if err != nil {
		return h, 0, err
	}

	return h, offset, nil
}

// HashesWithPrefix returns all hashes in the index that start with the given prefix.
// TODO: wait what?
func (idx *packIndex) HashesWithPrefix(prefix []byte) ([]plumbing.Hash, error) {
	if len(prefix) == 0 {
		return nil, nil
	}

	first := int(prefix[0])
	lo := 0
	if first > 0 {
		lo = int(idx.fanout[first-1])
	}
	hi := int(idx.fanout[first])

	nameBuf := make([]byte, idx.hashSize)
	start := hi
	for lo < hi {
		mid := lo + (hi-lo)/2
		nameOff := idx.namesOffset + int64(mid)*int64(idx.hashSize)
		if _, err := idx.file.ReadAt(nameBuf, nameOff); err != nil {
			return nil, fmt.Errorf("pack index: read name: %w", err)
		}

		cmp := bytes.Compare(nameBuf[:len(prefix)], prefix)
		if cmp < 0 {
			lo = mid + 1
		} else {
			start = mid
			hi = mid
		}
	}

	var hashes []plumbing.Hash
	for i := start; i < int(idx.fanout[first]); i++ {
		nameOff := idx.namesOffset + int64(i)*int64(idx.hashSize)
		if _, err := idx.file.ReadAt(nameBuf, nameOff); err != nil {
			return nil, fmt.Errorf("pack index: read name: %w", err)
		}

		if !bytes.HasPrefix(nameBuf, prefix) {
			break
		}

		var h plumbing.Hash
		h.ResetBySize(idx.hashSize)
		_, _ = h.Write(nameBuf)
		hashes = append(hashes, h)
	}

	return hashes, nil
}

// TODO: completely remove
func (idx *packIndex) FindHash(offset int64) (plumbing.Hash, error) {
	var buf4 [4]byte
	for i := 0; i < idx.numObjects; i++ {
		off := idx.offset32Offset + int64(i)*4
		if _, err := idx.file.ReadAt(buf4[:], off); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("pack index: read offset: %w", err)
		}

		word := binary.BigEndian.Uint32(buf4[:])
		var entryOffset int64
		if word&0x80000000 == 0 {
			entryOffset = int64(word)
		} else {
			pos := int(word & 0x7fffffff)
			if pos < 0 || pos >= idx.offset64Count {
				continue
			}
			var buf8 [8]byte
			off64 := idx.offset64Offset + int64(pos)*8
			if _, err := idx.file.ReadAt(buf8[:], off64); err != nil {
				continue
			}
			entryOffset = int64(binary.BigEndian.Uint64(buf8[:]))
		}

		if entryOffset == offset {
			var h plumbing.Hash
			h.ResetBySize(idx.hashSize)
			hashBuf := make([]byte, idx.hashSize)
			nameOff := idx.namesOffset + int64(i)*int64(idx.hashSize)
			if _, err := idx.file.ReadAt(hashBuf, nameOff); err != nil {
				return plumbing.ZeroHash, fmt.Errorf("pack index: read hash: %w", err)
			}
			_, _ = h.Write(hashBuf)
			return h, nil
		}
	}

	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

// Close closes the underlying idx file.
func (idx *packIndex) Close() error {
	if idx.file != nil {
		return idx.file.Close()
	}
	return nil
}
