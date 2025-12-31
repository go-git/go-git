package idxfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
)

// Exported constants for idx file structure
const (
	IdxHeaderSize = 8
	IdxFanoutSize = 256 * 4
	IdxCRCSize    = 4
	Off32Size     = 4
	Off64Size     = 8

	Is64BitsMask = uint64(1) << 31
)

// IdxHeader is the magic signature for idx files
var IdxHeader = []byte{255, 't', 'O', 'c'}

var (
	// ErrInvalidIdxFile is returned when the idx file has an invalid format
	ErrInvalidIdxFile = errors.New("invalid idx file")
)

// Buffer pools for reducing allocations during ReaderAt operations
var (
	pool4Bytes  = sync.Pool{New: func() interface{} { b := make([]byte, 4); return &b }}
	pool8Bytes  = sync.Pool{New: func() interface{} { b := make([]byte, 8); return &b }}
	pool20Bytes = sync.Pool{New: func() interface{} { b := make([]byte, 20); return &b }}
	pool32Bytes = sync.Pool{New: func() interface{} { b := make([]byte, 32); return &b }}
)

// OffsetLookup provides efficient offset-to-index lookups using a reverse index.
type OffsetLookup interface {
	// LookupIndex finds the index position for the given pack offset.
	// Returns the index position and true if found, or 0 and false if not found.
	LookupIndex(packOffset uint64, offsetGetter func(idxPos int) (uint64, error)) (int, bool)

	// LookupIndexWithCallback is like LookupIndex but calls onIntermediate for each
	// intermediate position visited during the binary search. This allows caching
	// of offset->idxPos mappings discovered during the search.
	// If onIntermediate is nil, behaves like LookupIndex.
	LookupIndexWithCallback(packOffset uint64, offsetGetter func(idxPos int) (uint64, error), onIntermediate func(offset uint64, idxPos int)) (int, bool)
}

// ReaderAtIndex implements Index using io.ReaderAt for on-demand access
// without loading the entire idx file into memory.
type ReaderAtIndex struct {
	reader    io.ReaderAt
	closer    io.Closer
	revCloser io.Closer
	hashSize  int
	count     int
	size      int64

	// Optional reverse index for efficient FindHash lookups
	revIndex OffsetLookup

	// Lazy-built offset->hash cache for FindHash
	offsetCache offsetHashCache

	// Cache for offset->idxPos mappings discovered during binary search.
	// This allows subsequent FindHash calls to skip the binary search.
	idxPosCache offsetIdxPosCache

	// Cached fanout table (256 entries, 1KB) for fast lookups
	fanout [256]uint32

	// Pre-computed structure offsets
	fanoutStart  int
	namesStart   int
	crcStart     int
	off32Start   int
	off64Start   int
	trailerStart int
}

var _ Index = (*ReaderAtIndex)(nil)

// IndexFile is an interface that combines the necessary methods for reading an index file.
// This is satisfied by billy.File and similar file types.
type IndexFile interface {
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
}

// NewReaderAtIndex creates a new Index from an index file.
//
// The idxFile parameter is the .idx file, which must implement io.ReaderAt, io.Closer, and Stat().
// The hashSize parameter specifies the size of object hashes (20 for SHA1, 32 for SHA256).
// The file will be closed when Close() is called on the returned Index.
//
// WARNING: Without a reverse index (set via SetRevIndex), FindHash() will perform an O(n)
// linear scan through all objects, which is extremely slow for large repositories
// (e.g., 40+ seconds for linux-kernel). Use SetRevIndex with a reverse index from the
// revfile package for O(log n) lookups.
func NewReaderAtIndex(idxFile IndexFile, hashSize int) (*ReaderAtIndex, error) {
	idxStat, err := idxFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat idx file: %w", err)
	}

	idx := &ReaderAtIndex{
		reader:   idxFile,
		closer:   idxFile,
		hashSize: hashSize,
		size:     idxStat.Size(),
	}

	if err := idx.init(); err != nil {
		_ = idxFile.Close()
		return nil, err
	}

	return idx, nil
}

// SetRevIndex sets the reverse index for efficient FindHash lookups.
// Without a reverse index, FindHash performs a linear scan O(n).
// With a reverse index, FindHash uses binary search O(log n).
//
// If the revIndex implements io.Closer, it will be closed when Close() is called.
func (idx *ReaderAtIndex) SetRevIndex(rev OffsetLookup) {
	idx.revIndex = rev
	if closer, ok := rev.(io.Closer); ok {
		idx.revCloser = closer
	}
}

func (idx *ReaderAtIndex) init() error {
	minLen := int64(IdxHeaderSize + IdxFanoutSize + IdxCRCSize + len(IdxHeader) + 40)
	if idx.size < minLen {
		return fmt.Errorf("%w: file too small", ErrInvalidIdxFile)
	}

	// Validate header
	header := make([]byte, len(IdxHeader)+4)
	n, err := idx.reader.ReadAt(header, 0)
	if err != nil {
		return fmt.Errorf("%w: failed to read header: %w", ErrInvalidIdxFile, err)
	}
	if n != len(header) {
		return fmt.Errorf("%w: short read on header", ErrInvalidIdxFile)
	}

	if !bytes.Equal(IdxHeader, header[:len(IdxHeader)]) {
		return fmt.Errorf("%w: invalid signature", ErrInvalidIdxFile)
	}

	version := binary.BigEndian.Uint32(header[len(IdxHeader):])
	if version != VersionSupported {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidIdxFile, version)
	}

	// Read and cache the entire fanout table (256 entries * 4 bytes = 1KB)
	// This avoids repeated I/O for fanout lookups during FindOffset.
	fanoutBuf := make([]byte, IdxFanoutSize)
	n, err = idx.reader.ReadAt(fanoutBuf, int64(IdxHeaderSize))
	if err != nil {
		return fmt.Errorf("%w: failed to read fanout table: %w", ErrInvalidIdxFile, err)
	}
	if n != IdxFanoutSize {
		return fmt.Errorf("%w: short read from fanout table", ErrInvalidIdxFile)
	}

	// Parse fanout table into cached array
	for i := 0; i < 256; i++ {
		idx.fanout[i] = binary.BigEndian.Uint32(fanoutBuf[i*4 : (i+1)*4])
	}

	idx.count = int(idx.fanout[255])
	idx.fanoutStart = IdxHeaderSize
	idx.namesStart = idx.fanoutStart + IdxFanoutSize
	idx.crcStart = idx.namesStart + (idx.count * idx.hashSize)
	idx.off32Start = idx.crcStart + (idx.count * IdxCRCSize)
	idx.off64Start = idx.off32Start + (idx.count * Off32Size)
	idx.trailerStart = int(idx.size) - 2*idx.hashSize

	return nil
}

// Close implements the Index interface.
// It closes both the idx file and the rev file (if provided).
func (idx *ReaderAtIndex) Close() error {
	var errs []error
	if idx.revCloser != nil {
		if err := idx.revCloser.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if idx.closer != nil {
		if err := idx.closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Contains implements the Index interface.
func (idx *ReaderAtIndex) Contains(h plumbing.Hash) (bool, error) {
	_, err := idx.FindOffset(h)
	if err == plumbing.ErrObjectNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// FindOffset implements the Index interface.
func (idx *ReaderAtIndex) FindOffset(h plumbing.Hash) (int64, error) {
	first := int(h.Bytes()[0])
	var lo int
	if first > 0 {
		lo = int(idx.fanoutEntry(first - 1))
	}
	hi := int(idx.fanoutEntry(first))

	pos, found := idx.searchHash(lo, hi, h)
	if !found {
		return 0, plumbing.ErrObjectNotFound
	}

	offset, err := idx.offset(pos)
	if err != nil {
		return 0, err
	}

	return int64(offset), nil
}

// FindCRC32 implements the Index interface.
func (idx *ReaderAtIndex) FindCRC32(h plumbing.Hash) (uint32, error) {
	first := int(h.Bytes()[0])
	var lo int
	if first > 0 {
		lo = int(idx.fanoutEntry(first - 1))
	}
	hi := int(idx.fanoutEntry(first))

	pos, found := idx.searchHash(lo, hi, h)
	if !found {
		return 0, plumbing.ErrObjectNotFound
	}

	return idx.crc32(pos)
}

// FindHash implements the Index interface.
// If a reverse index is set via SetRevIndex, this uses O(log n) binary search.
// Otherwise, it builds a hash map lazily (once) for O(1) lookups.
func (idx *ReaderAtIndex) FindHash(o int64) (plumbing.Hash, error) {
	// Check hash cache first (works for both revIndex and fallback paths)
	if hash, ok := idx.offsetCache.Get(o); ok {
		return hash, nil
	}

	// Use reverse index if available for O(log n) lookup
	if idx.revIndex != nil {
		// Check if we have a cached idxPos from a previous binary search
		if idxPos, ok := idx.idxPosCache.Get(o); ok {
			hash, err := idx.hashAt(idxPos)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			idx.offsetCache.Put(o, hash)
			return hash, nil
		}

		// Perform binary search, caching intermediate offset->idxPos mappings
		pos, found := idx.revIndex.LookupIndexWithCallback(uint64(o),
			func(idxPos int) (uint64, error) {
				return idx.offset(idxPos)
			},
			func(offset uint64, idxPos int) {
				// Cache intermediate offset->idxPos mapping for future lookups
				idx.idxPosCache.Put(int64(offset), idxPos)
			},
		)
		if found {
			hash, err := idx.hashAt(pos)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			// Cache for future lookups
			idx.offsetCache.Put(o, hash)
			return hash, nil
		}
		return plumbing.ZeroHash, plumbing.ErrObjectNotFound
	}

	// Fallback: Build offset->hash map once (like MemoryIndex)
	if err := idx.offsetCache.BuildOnce(idx.buildOffsetHash); err != nil {
		return plumbing.ZeroHash, err
	}

	if hash, ok := idx.offsetCache.Get(o); ok {
		return hash, nil
	}
	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

// buildOffsetHash builds the complete offset->hash map for fallback FindHash.
// This is called once lazily when FindHash is used without a revIndex.
func (idx *ReaderAtIndex) buildOffsetHash() (map[int64]plumbing.Hash, error) {
	offsetHash := make(map[int64]plumbing.Hash, idx.count)

	for i := 0; i < idx.count; i++ {
		hash, err := idx.hashAt(i)
		if err != nil {
			return nil, err
		}
		offset, err := idx.offset(i)
		if err != nil {
			return nil, err
		}
		offsetHash[int64(offset)] = hash
	}

	return offsetHash, nil
}

// Count implements the Index interface.
func (idx *ReaderAtIndex) Count() (int64, error) {
	return int64(idx.count), nil
}

// Entries implements the Index interface.
func (idx *ReaderAtIndex) Entries() (EntryIter, error) {
	return &readerAtEntryIter{idx: idx, pos: 0}, nil
}

// EntriesByOffset implements the Index interface.
func (idx *ReaderAtIndex) EntriesByOffset() (EntryIter, error) {
	count := idx.count
	entries := make(entriesByOffset, count)

	for i := 0; i < count; i++ {
		entry, err := idx.entryAt(i)
		if err != nil {
			return nil, err
		}
		entries[i] = entry
	}

	sort.Sort(entries)
	return &idxfileEntryOffsetIter{entries: entries}, nil
}

// fanoutEntry returns the value at index i in the fanout table.
// fanoutEntry returns the value at index i in the cached fanout table.
func (idx *ReaderAtIndex) fanoutEntry(i int) uint32 {
	if i < 0 || i >= 256 {
		return 0
	}
	return idx.fanout[i]
}

// searchHash performs a binary search for a hash in the names table.
func (idx *ReaderAtIndex) searchHash(left, right int, want plumbing.Hash) (int, bool) {
	wantBytes := want.Bytes()
	n := right - left

	pos := left + sort.Search(n, func(i int) bool {
		return idx.compareHash(left+i, wantBytes) >= 0
	})

	if pos < right && idx.compareHash(pos, wantBytes) == 0 {
		return pos, true
	}

	return 0, false
}

// compareHash compares a hash at the given index with the wanted bytes.
func (idx *ReaderAtIndex) compareHash(i int, want []byte) int {
	offset := int64(idx.namesStart + (i * idx.hashSize))

	var bufPtr *[]byte
	var pool *sync.Pool
	if idx.hashSize == 20 {
		bufPtr = pool20Bytes.Get().(*[]byte)
		pool = &pool20Bytes
	} else {
		bufPtr = pool32Bytes.Get().(*[]byte)
		pool = &pool32Bytes
	}
	buf := (*bufPtr)[:idx.hashSize]
	defer pool.Put(bufPtr)

	n, err := idx.reader.ReadAt(buf, offset)
	if err != nil || n != idx.hashSize {
		return -1
	}

	return bytes.Compare(buf, want)
}

// offset returns the pack offset for the object at the given index position.
func (idx *ReaderAtIndex) offset(pos int) (uint64, error) {
	start := int64(idx.off32Start + pos*Off32Size)

	bufPtr := pool4Bytes.Get().(*[]byte)
	off32Buf := *bufPtr
	defer pool4Bytes.Put(bufPtr)

	n, err := idx.reader.ReadAt(off32Buf, start)
	if err != nil {
		return 0, fmt.Errorf("failed to read offset32: %w", err)
	}
	if n != Off32Size {
		return 0, fmt.Errorf("short read for offset32")
	}

	off32 := binary.BigEndian.Uint32(off32Buf)

	if uint64(off32)&Is64BitsMask != 0 {
		loIndex := int(uint64(off32) & ^Is64BitsMask)
		start := int64(idx.off64Start + (loIndex * Off64Size))

		bufPtr64 := pool8Bytes.Get().(*[]byte)
		off64Buf := *bufPtr64
		defer pool8Bytes.Put(bufPtr64)

		n, err := idx.reader.ReadAt(off64Buf, start)
		if err != nil {
			return 0, fmt.Errorf("failed to read offset64: %w", err)
		}
		if n != Off64Size {
			return 0, fmt.Errorf("short read for offset64")
		}

		return binary.BigEndian.Uint64(off64Buf), nil
	}

	return uint64(off32), nil
}

// crc32 returns the CRC32 checksum for the object at the given index position.
func (idx *ReaderAtIndex) crc32(pos int) (uint32, error) {
	start := int64(idx.crcStart + pos*IdxCRCSize)

	bufPtr := pool4Bytes.Get().(*[]byte)
	buf := *bufPtr
	defer pool4Bytes.Put(bufPtr)

	n, err := idx.reader.ReadAt(buf, start)
	if err != nil {
		return 0, fmt.Errorf("failed to read CRC32: %w", err)
	}
	if n != IdxCRCSize {
		return 0, fmt.Errorf("short read for CRC32")
	}

	return binary.BigEndian.Uint32(buf), nil
}

// hashAt returns the hash at the given index position.
func (idx *ReaderAtIndex) hashAt(pos int) (plumbing.Hash, error) {
	offset := int64(idx.namesStart + (pos * idx.hashSize))

	var bufPtr *[]byte
	var pool *sync.Pool
	if idx.hashSize == 20 {
		bufPtr = pool20Bytes.Get().(*[]byte)
		pool = &pool20Bytes
	} else {
		bufPtr = pool32Bytes.Get().(*[]byte)
		pool = &pool32Bytes
	}
	hashBuf := (*bufPtr)[:idx.hashSize]
	defer pool.Put(bufPtr)

	n, err := idx.reader.ReadAt(hashBuf, offset)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to read hash: %w", err)
	}
	if n != idx.hashSize {
		return plumbing.ZeroHash, fmt.Errorf("short read for hash")
	}

	var h plumbing.Hash
	h.ResetBySize(idx.hashSize)
	_, _ = h.Write(hashBuf)
	return h, nil
}

// entryAt returns the entry at the given index position.
func (idx *ReaderAtIndex) entryAt(pos int) (*Entry, error) {
	hash, err := idx.hashAt(pos)
	if err != nil {
		return nil, err
	}

	offset, err := idx.offset(pos)
	if err != nil {
		return nil, err
	}

	crc, err := idx.crc32(pos)
	if err != nil {
		return nil, err
	}

	return &Entry{
		Hash:   hash,
		Offset: offset,
		CRC32:  crc,
	}, nil
}

// readerAtEntryIter implements EntryIter for ReaderAtIndex.
type readerAtEntryIter struct {
	idx *ReaderAtIndex
	pos int
}

func (i *readerAtEntryIter) Next() (*Entry, error) {
	if i.pos >= i.idx.count {
		return nil, io.EOF
	}

	entry, err := i.idx.entryAt(i.pos)
	if err != nil {
		return nil, err
	}

	i.pos++
	return entry, nil
}

func (i *readerAtEntryIter) Close() error {
	i.pos = i.idx.count + 1
	return nil
}
