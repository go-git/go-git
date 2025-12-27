package revfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
)

// Exported constants for rev file structure
const (
	RevHeaderSize    = 12 // magic (4) + version (4) + hash function (4)
	RevEntrySize     = 4  // each entry is a 4-byte index position
	RevVersionOffset = 4
	RevHashFnOffset  = 8
)

// RevHeader is the magic signature for rev files
var RevHeader = []byte{'R', 'I', 'D', 'X'}

var (
	// ErrInvalidRevFile is returned when the rev file has an invalid format
	ErrInvalidRevFile = errors.New("invalid rev file")
)

// Buffer pool for reducing allocations
var revPool4Bytes = sync.Pool{New: func() interface{} { b := make([]byte, 4); return &b }}

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
	// Check minimum size: header + entries + trailer (2 hashes)
	minSize := int64(RevHeaderSize) + ri.count*int64(RevEntrySize) + int64(2*ri.hashSize)
	if ri.size < minSize {
		return fmt.Errorf("%w: file too small", ErrInvalidRevFile)
	}

	// Validate header
	header := make([]byte, RevHeaderSize)
	n, err := ri.reader.ReadAt(header, 0)
	if err != nil {
		return fmt.Errorf("%w: failed to read header: %w", ErrInvalidRevFile, err)
	}
	if n != RevHeaderSize {
		return fmt.Errorf("%w: short read on header", ErrInvalidRevFile)
	}

	// Check magic
	if !bytes.Equal(RevHeader, header[:4]) {
		return fmt.Errorf("%w: invalid signature", ErrInvalidRevFile)
	}

	// Check version
	version := binary.BigEndian.Uint32(header[RevVersionOffset:])
	if version != VersionSupported {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidRevFile, version)
	}

	// Check hash function
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

	// Verify expected size
	expectedSize := int64(RevHeaderSize) + ri.count*RevEntrySize + int64(2*ri.hashSize)
	if ri.size != expectedSize {
		return fmt.Errorf("%w: size mismatch (expected %d, got %d)", ErrInvalidRevFile, expectedSize, ri.size)
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

// GetIndexPosition returns the index position at the given rev file position.
// The position is 0-indexed and corresponds to the sorted pack offset order.
func (ri *ReaderAtRevIndex) GetIndexPosition(pos int) (int, error) {
	if pos < 0 || int64(pos) >= ri.count {
		return 0, fmt.Errorf("position %d out of range [0, %d)", pos, ri.count)
	}

	bufPtr := revPool4Bytes.Get().(*[]byte)
	buf := *bufPtr
	defer revPool4Bytes.Put(bufPtr)

	offset := int64(RevHeaderSize + pos*RevEntrySize)
	n, err := ri.reader.ReadAt(buf, offset)
	if err != nil {
		return 0, fmt.Errorf("failed to read index position: %w", err)
	}
	if n != RevEntrySize {
		return 0, fmt.Errorf("short read for index position")
	}

	return int(binary.BigEndian.Uint32(buf)), nil
}
