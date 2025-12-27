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

// ReaderAtIndex implements Index using io.ReaderAt for on-demand access
// without loading the entire idx file into memory.
type ReaderAtIndex struct {
	reader   io.ReaderAt
	closer   io.Closer
	hashSize int
	count    int
	size     int64

	// Lazy-built offset->hash map for FindHash
	offsetHash      map[int64]plumbing.Hash
	offsetBuildOnce sync.Once
	mu              sync.RWMutex

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
func (idx *ReaderAtIndex) Close() error {
	if idx.closer != nil {
		return idx.closer.Close()
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

	// Cache for reverse lookup
	idx.mu.Lock()
	if idx.offsetHash == nil {
		idx.offsetHash = make(map[int64]plumbing.Hash)
	}
	idx.offsetHash[int64(offset)] = h
	idx.mu.Unlock()

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
// Without a reverse index, this builds a complete offset->hash map lazily.
func (idx *ReaderAtIndex) FindHash(o int64) (plumbing.Hash, error) {
	// Check cache first
	idx.mu.RLock()
	if idx.offsetHash != nil {
		if hash, ok := idx.offsetHash[o]; ok {
			idx.mu.RUnlock()
			return hash, nil
		}
	}
	idx.mu.RUnlock()

	// Build complete map once
	var buildErr error
	idx.offsetBuildOnce.Do(func() {
		buildErr = idx.buildOffsetHash()
	})
	if buildErr != nil {
		return plumbing.ZeroHash, buildErr
	}

	idx.mu.RLock()
	hash, ok := idx.offsetHash[o]
	idx.mu.RUnlock()

	if !ok {
		return plumbing.ZeroHash, plumbing.ErrObjectNotFound
	}
	return hash, nil
}

// buildOffsetHash builds the complete offset->hash map for FindHash.
func (idx *ReaderAtIndex) buildOffsetHash() error {
	offsetHash := make(map[int64]plumbing.Hash, idx.count)

	for i := 0; i < idx.count; i++ {
		hash, err := idx.hashAt(i)
		if err != nil {
			return err
		}

		offset, err := idx.offset(i)
		if err != nil {
			return err
		}

		offsetHash[int64(offset)] = hash
	}

	idx.mu.Lock()
	idx.offsetHash = offsetHash
	idx.mu.Unlock()

	return nil
}

// Count implements the Index interface.
func (idx *ReaderAtIndex) Count() (int64, error) {
	return int64(idx.count), nil
}

// Entries implements the Index interface.
func (idx *ReaderAtIndex) Entries() (EntryIter, error) {
	return &readerAtEntryIter{idx: idx, total: idx.count}, nil
}

// EntriesByOffset implements the Index interface.
func (idx *ReaderAtIndex) EntriesByOffset() (EntryIter, error) {
	entries := make(entriesByOffset, idx.count)

	for i := 0; i < idx.count; i++ {
		hash, err := idx.hashAt(i)
		if err != nil {
			return nil, err
		}

		offset, err := idx.offset(i)
		if err != nil {
			return nil, err
		}

		crc, err := idx.crc32(i)
		if err != nil {
			return nil, err
		}

		entries[i] = &Entry{
			Hash:   hash,
			CRC32:  crc,
			Offset: offset,
		}
	}

	sort.Sort(entries)

	return &sliceEntryIter{entries: entries}, nil
}

// fanoutEntry returns the fanout value for the given byte.
func (idx *ReaderAtIndex) fanoutEntry(b int) uint32 {
	return idx.fanout[b]
}

// searchHash performs a binary search for the given hash in the specified range.
func (idx *ReaderAtIndex) searchHash(lo, hi int, want plumbing.Hash) (int, bool) {
	wantBytes := want.Bytes()
	right := hi

	pos := sort.Search(hi-lo, func(i int) bool {
		cmp := idx.compareHash(lo+i, wantBytes)
		return cmp >= 0
	})

	pos += lo

	if pos < right && idx.compareHash(pos, wantBytes) == 0 {
		return pos, true
	}

	return 0, false
}

// compareHash compares a hash at the given index with the wanted bytes.
func (idx *ReaderAtIndex) compareHash(i int, want []byte) int {
	offset := int64(idx.namesStart + (i * idx.hashSize))

	bufPtr := idx.getHashBuffer()
	buf := (*bufPtr)[:idx.hashSize]
	defer idx.putHashBuffer(bufPtr)

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

	bufPtr := idx.getHashBuffer()
	hashBuf := (*bufPtr)[:idx.hashSize]
	defer idx.putHashBuffer(bufPtr)

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

// getHashBuffer returns a buffer from the appropriate pool based on hash size.
func (idx *ReaderAtIndex) getHashBuffer() *[]byte {
	if idx.hashSize == 20 {
		return pool20Bytes.Get().(*[]byte)
	}
	return pool32Bytes.Get().(*[]byte)
}

// putHashBuffer returns a buffer to the appropriate pool based on hash size.
func (idx *ReaderAtIndex) putHashBuffer(buf *[]byte) {
	if idx.hashSize == 20 {
		pool20Bytes.Put(buf)
	} else {
		pool32Bytes.Put(buf)
	}
}

// readerAtEntryIter iterates over entries in a ReaderAtIndex.
type readerAtEntryIter struct {
	idx   *ReaderAtIndex
	pos   int
	total int
}

func (i *readerAtEntryIter) Next() (*Entry, error) {
	if i.pos >= i.total {
		return nil, io.EOF
	}

	hash, err := i.idx.hashAt(i.pos)
	if err != nil {
		return nil, err
	}

	offset, err := i.idx.offset(i.pos)
	if err != nil {
		return nil, err
	}

	crc, err := i.idx.crc32(i.pos)
	if err != nil {
		return nil, err
	}

	i.pos++

	return &Entry{
		Hash:   hash,
		CRC32:  crc,
		Offset: offset,
	}, nil
}

func (i *readerAtEntryIter) Close() error {
	return nil
}

// sliceEntryIter iterates over a pre-sorted slice of entries.
type sliceEntryIter struct {
	entries []*Entry
	pos     int
}

func (i *sliceEntryIter) Next() (*Entry, error) {
	if i.pos >= len(i.entries) {
		return nil, io.EOF
	}
	e := i.entries[i.pos]
	i.pos++
	return e, nil
}

func (i *sliceEntryIter) Close() error {
	return nil
}
