package revfile

import (
	"bytes"
	"crypto"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"iter"
	"sync"
)

// Exported constants for rev file structure.
const (
	RevHeaderSize    = 12 // magic (4) + version (4) + hash function (4)
	RevEntrySize     = 4  // each entry is a 4-byte index position
	RevVersionOffset = 4
	RevHashFnOffset  = 8
)

// Revfile constants.
const (
	VersionSupported        = 1
	sha1Hash         uint32 = 1
	sha256Hash       uint32 = 2
)

// revHeader is the magic signature for rev files.
var revHeader = []byte{'R', 'I', 'D', 'X'}

// ErrInvalidRevFile is returned when the rev file has an invalid format.
var ErrInvalidRevFile = errors.New("invalid rev file")

// maxObjectCount is a reasonable upper bound for the number of objects.
// Git packfiles are limited to ~4 billion objects (2^32), but in practice
// even extremely large repositories have far fewer objects.
const maxObjectCount = 1 << 31 // ~2 billion objects

// Buffer pool for reducing allocations.
var revPool4Bytes = sync.Pool{New: func() any { b := make([]byte, 4); return &b }}

// ReaderAtRevIndex provides offset-to-index lookups using io.ReaderAt
// for the reverse index (.rev) file.
//
// The reverse index maps pack file offsets (sorted) to index positions,
// allowing efficient lookup of which object is at a given pack offset.
type ReaderAtRevIndex struct {
	reader   io.ReaderAt
	closer   io.Closer
	hashSize int
	count    int64
	size     int64
}

// RevFile is an interface that combines the necessary methods for reading a rev file.
// This is satisfied by billy.File and similar file types.
type RevFile interface {
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
}

// NewReaderAtRevIndex creates a new reverse index from a .rev file.
//
// The revFile parameter is the .rev file, which must implement io.ReaderAt, io.Closer, and Stat().
// The hashSize parameter specifies the size of object hashes (20 for SHA1, 32 for SHA256).
// The count parameter is the number of objects in the index (from the .idx file).
// The revFile will be closed when Close() is called on the returned index.
func NewReaderAtRevIndex(revFile RevFile, hashSize int, count int64) (*ReaderAtRevIndex, error) {
	// Validate hashSize early - only SHA1 (20) and SHA256 (32) are supported.
	if hashSize != 20 && hashSize != 32 {
		return nil, fmt.Errorf("%w: invalid hash size %d (must be 20 for SHA1 or 32 for SHA256)", ErrInvalidRevFile, hashSize)
	}

	// Validate count to prevent integer overflow in size calculations.
	if count < 0 || count > maxObjectCount {
		return nil, fmt.Errorf("%w: invalid object count %d", ErrInvalidRevFile, count)
	}

	stat, err := revFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat rev file: %w", err)
	}

	ri := &ReaderAtRevIndex{
		reader:   revFile,
		closer:   revFile,
		hashSize: hashSize,
		count:    count,
		size:     stat.Size(),
	}

	if err := ri.validate(); err != nil {
		_ = revFile.Close()
		return nil, err
	}

	return ri, nil
}

func (ri *ReaderAtRevIndex) validate() error {
	minSize := int64(RevHeaderSize) + ri.count*int64(RevEntrySize) + int64(2*ri.hashSize)
	if ri.size < minSize {
		return fmt.Errorf("%w: file too small", ErrInvalidRevFile)
	}

	header := make([]byte, RevHeaderSize)
	n, err := ri.reader.ReadAt(header, 0)
	if err != nil {
		return fmt.Errorf("%w: failed to read header: %w", ErrInvalidRevFile, err)
	}
	if n != RevHeaderSize {
		return fmt.Errorf("%w: short read on header", ErrInvalidRevFile)
	}

	if !bytes.Equal(revHeader, header[:4]) {
		return fmt.Errorf("%w: invalid signature", ErrInvalidRevFile)
	}

	version := binary.BigEndian.Uint32(header[RevVersionOffset:])
	if version != VersionSupported {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidRevFile, version)
	}

	hashFn := binary.BigEndian.Uint32(header[RevHashFnOffset:])
	switch hashFn {
	case sha1Hash:
		if ri.hashSize != 20 {
			return fmt.Errorf("%w: hash size mismatch (expected SHA1)", ErrInvalidRevFile)
		}
	case sha256Hash:
		if ri.hashSize != 32 {
			return fmt.Errorf("%w: hash size mismatch (expected SHA256)", ErrInvalidRevFile)
		}
	default:
		return fmt.Errorf("%w: unsupported hash function %d", ErrInvalidRevFile, hashFn)
	}

	// Verify expected size.
	expectedSize := int64(RevHeaderSize) + ri.count*RevEntrySize + int64(2*ri.hashSize)
	if ri.size != expectedSize {
		return fmt.Errorf("%w: size mismatch (expected %d, got %d)", ErrInvalidRevFile, expectedSize, ri.size)
	}

	return nil
}

// ValidateChecksums verifies the integrity of the rev file by:
//  1. Computing the hash of all data (header + entries + pack checksum)
//     and comparing it with the stored rev file checksum.
//  2. Comparing the stored pack file checksum with the provided packChecksum.
//
// This requires reading the entire file and should only be called when
// explicit integrity verification is needed.
func (ri *ReaderAtRevIndex) ValidateChecksums(packChecksum []byte) error {
	if len(packChecksum) != ri.hashSize {
		return fmt.Errorf("%w: pack checksum size mismatch (expected %d, got %d)",
			ErrInvalidRevFile, ri.hashSize, len(packChecksum))
	}

	// Determine hash function based on hash size.
	var hasher crypto.Hash
	switch ri.hashSize {
	case 20:
		hasher = crypto.SHA1
	case 32:
		hasher = crypto.SHA256
	default:
		return fmt.Errorf("%w: unsupported hash size %d", ErrInvalidRevFile, ri.hashSize)
	}

	h := hasher.New()

	// Read and hash all data except the final rev checksum.
	dataSize := ri.size - int64(ri.hashSize)
	if err := ri.hashData(h, 0, dataSize); err != nil {
		return err
	}

	// Read the stored pack checksum and verify it.
	packChecksumOffset := ri.size - int64(2*ri.hashSize)
	storedPackChecksum := make([]byte, ri.hashSize)
	n, err := ri.reader.ReadAt(storedPackChecksum, packChecksumOffset)
	if err != nil {
		return fmt.Errorf("%w: failed to read pack checksum: %w", ErrInvalidRevFile, err)
	}
	if n != ri.hashSize {
		return fmt.Errorf("%w: short read on pack checksum", ErrInvalidRevFile)
	}

	if !bytes.Equal(storedPackChecksum, packChecksum) {
		return fmt.Errorf("%w: pack checksum mismatch", ErrInvalidRevFile)
	}

	// Read the stored rev checksum.
	revChecksumOffset := ri.size - int64(ri.hashSize)
	storedRevChecksum := make([]byte, ri.hashSize)
	n, err = ri.reader.ReadAt(storedRevChecksum, revChecksumOffset)
	if err != nil {
		return fmt.Errorf("%w: failed to read rev checksum: %w", ErrInvalidRevFile, err)
	}
	if n != ri.hashSize {
		return fmt.Errorf("%w: short read on rev checksum", ErrInvalidRevFile)
	}

	// Compare computed hash with stored rev checksum.
	computedChecksum := h.Sum(nil)
	if !bytes.Equal(computedChecksum, storedRevChecksum) {
		return fmt.Errorf("%w: rev file checksum mismatch", ErrInvalidRevFile)
	}

	return nil
}

// hashData reads data from the reader and writes it to the hash.
func (ri *ReaderAtRevIndex) hashData(h hash.Hash, offset, length int64) error {
	const bufSize = 32 * 1024 // 32KB buffer
	buf := make([]byte, bufSize)

	for length > 0 {
		toRead := min(int64(bufSize), length)

		n, err := ri.reader.ReadAt(buf[:toRead], offset)
		if err != nil && err != io.EOF {
			return fmt.Errorf("%w: failed to read data at offset %d: %w", ErrInvalidRevFile, offset, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: unexpected EOF at offset %d", ErrInvalidRevFile, offset)
		}

		_, err = h.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("failed to hash data: %w", err)
		}

		offset += int64(n)
		length -= int64(n)
	}

	return nil
}

// Close releases resources associated with the reverse index.
func (ri *ReaderAtRevIndex) Close() error {
	if ri.closer != nil {
		return ri.closer.Close()
	}
	return nil
}

// Count returns the number of entries in the reverse index.
func (ri *ReaderAtRevIndex) Count() int64 {
	return ri.count
}

// LookupIndex performs a binary search to find the index position
// that corresponds to a given pack offset.
//
// The offsetGetter function is called with an index position and should
// return the pack offset for that position. This is typically retrieved
// from the idx file's offset table.
//
// Returns the index position and true if found, or 0 and false if not found.
func (ri *ReaderAtRevIndex) LookupIndex(packOffset uint64, offsetGetter func(idxPos int) (uint64, error)) (int, bool) {
	if ri.count == 0 {
		return 0, false
	}

	left, right := 0, int(ri.count)-1

	bufPtr := revPool4Bytes.Get().(*[]byte)
	buf := *bufPtr
	defer revPool4Bytes.Put(bufPtr)

	for left <= right {
		mid := (left + right) / 2

		offset := int64(RevHeaderSize + mid*RevEntrySize)
		n, err := ri.reader.ReadAt(buf, offset)
		if err != nil || n != RevEntrySize {
			return 0, false
		}

		idxPos := int(binary.BigEndian.Uint32(buf))
		got, err := offsetGetter(idxPos)
		if err != nil {
			return 0, false
		}

		switch {
		case got == packOffset:
			return idxPos, true
		case got < packOffset:
			left = mid + 1
		default:
			right = mid - 1
		}
	}

	return 0, false
}

// All returns an iterator over all (revPosition, idxPosition) pairs in the reverse index,
// along with a finish function that returns any error encountered during iteration.
//
// The revPosition is the position in pack offset order (0-indexed),
// and idxPosition is the corresponding position in the index file.
//
// Usage:
//
//	seq, finish := ri.All()
//	for revPos, idxPos := range seq {
//	    // process entries
//	}
//	if err := finish(); err != nil {
//	    // handle error
//	}
func (ri *ReaderAtRevIndex) All() (iter.Seq2[int, int], func() error) {
	var iterErr error

	seq := func(yield func(int, int) bool) {
		buf := make([]byte, 4)
		for i := 0; i < int(ri.count); i++ {
			offset := int64(RevHeaderSize + i*RevEntrySize)
			n, err := ri.reader.ReadAt(buf, offset)
			if err != nil {
				iterErr = fmt.Errorf("failed to read entry %d: %w", i, err)
				return
			}
			if n != RevEntrySize {
				iterErr = fmt.Errorf("short read for entry %d", i)
				return
			}
			idxPos := int(binary.BigEndian.Uint32(buf))
			if !yield(i, idxPos) {
				return
			}
		}
	}

	return seq, func() error { return iterErr }
}
