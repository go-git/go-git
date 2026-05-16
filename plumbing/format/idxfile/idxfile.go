package idxfile

import (
	"bytes"
	"crypto"
	encbin "encoding/binary"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
)

const (
	// VersionSupported is the only idx version supported.
	VersionSupported = 2

	noMapping = -1
)

var idxHeader = []byte{255, 't', 'O', 'c'}

// Index represents an index of a packfile.
//
// Implementations satisfy a [io.Closer] contract via [Index.Close]:
// on-disk implementations release file descriptors, pure
// in-memory implementations return nil. Downstream callers
// holding their own concrete [Index] implementations must
// supply a [Close] method to satisfy this interface; a no-op
// `func (*MyIndex) Close() error { return nil }` is sufficient
// for in-memory backends.
type Index interface {
	// Contains checks whether the given hash is in the index.
	Contains(h plumbing.Hash) (bool, error)
	// FindOffset finds the offset in the packfile for the object with
	// the given hash.
	FindOffset(h plumbing.Hash) (int64, error)
	// FindCRC32 finds the CRC32 of the object with the given hash.
	FindCRC32(h plumbing.Hash) (uint32, error)
	// FindHash finds the hash for the object with the given offset.
	FindHash(o int64) (plumbing.Hash, error)
	// Count returns the number of entries in the index.
	Count() (int64, error)
	// Entries returns an iterator to retrieve all index entries.
	Entries() (EntryIter, error)
	// EntriesByOffset returns an iterator to retrieve all index entries ordered
	// by offset.
	EntriesByOffset() (EntryIter, error)
	// EntriesWithPrefix returns an iterator over index entries whose
	// hashes start with prefix. Implementations use the fanout table
	// to bound the search when len(prefix) >= 1; an empty prefix
	// returns all entries (equivalent to Entries). The returned
	// iterator must be Closed by the caller to release any held
	// resources.
	EntriesWithPrefix(prefix []byte) (EntryIter, error)
	// MayContain reports whether the index might contain h. A false
	// return is authoritative ("h is definitely not in this pack")
	// based on the idx fanout table; true means the caller should
	// call Contains or FindOffset for a definitive answer.
	//
	// Implementations must be O(1) and I/O-free. Callers route
	// every read through MayContain to gate further index work
	// (see storage/filesystem.ObjectStorage.findObjectInPackfile);
	// an implementation that performs I/O or scales with index
	// size silently regresses every storage-level read.
	MayContain(h plumbing.Hash) bool
	// Close releases any resources held by the index. Implementations
	// backed by on-disk files must close their file descriptors; pure
	// in-memory implementations must return nil. Close is idempotent.
	Close() error
}

// MemoryIndex is the in memory representation of an idx file.
//
// The use of MemoryIndex for large repositories is discouraged.
// Use [LazyIndex] instead.
type MemoryIndex struct {
	// Version is the version of the index file.
	Version uint32
	// Fanout is a table where the Nth entry is the cumulative count of objects with the first byte of their name <= N.
	Fanout [256]uint32
	// FanoutMapping maps the position in the fanout table to the position
	// in the Names, Offset32 and CRC32 slices. This improves the memory
	// usage by not needing an array with unnecessary empty slots.
	FanoutMapping [256]int
	// Names is the list of object names.
	Names [][]byte
	// Offset32 is the list of 32-bit offsets.
	Offset32 [][]byte
	// CRC32 is the list of CRC32 checksums.
	CRC32 [][]byte
	// Offset64 is the list of 64-bit offsets.
	Offset64 []byte
	// PackfileChecksum is the checksum of the packfile.
	PackfileChecksum plumbing.Hash
	// IdxChecksum is the checksum of the index file.
	IdxChecksum plumbing.Hash

	offsetHash      map[int64]plumbing.Hash
	offsetBuildOnce sync.Once
	mu              sync.RWMutex

	objectIDSize int
}

var _ Index = (*MemoryIndex)(nil)

// Close is a no-op. MemoryIndex holds no external resources.
func (idx *MemoryIndex) Close() error { return nil }

// NewMemoryIndex returns an instance of a new MemoryIndex.
func NewMemoryIndex(objectIDSize int) *MemoryIndex {
	m := &MemoryIndex{objectIDSize: objectIDSize}
	m.IdxChecksum.ResetBySize(objectIDSize)
	m.PackfileChecksum.ResetBySize(objectIDSize)
	return m
}

func (idx *MemoryIndex) findHashIndex(h plumbing.Hash) (int, bool) {
	k := idx.FanoutMapping[h.Bytes()[0]]
	if k == noMapping {
		return 0, false
	}

	if len(idx.Names) <= k {
		return 0, false
	}

	data := idx.Names[k]
	high := uint64(len(idx.Offset32[k])) >> 2
	if high == 0 {
		return 0, false
	}

	low := uint64(0)
	for {
		mid := (low + high) >> 1
		offset := mid * uint64(idx.idSize())

		cmp := h.Compare(data[offset : offset+uint64(idx.idSize())])
		switch {
		case cmp < 0:
			high = mid
		case cmp == 0:
			return int(mid), true
		default:
			low = mid + 1
		}

		if low >= high {
			break
		}
	}

	return 0, false
}

// MayContain implements the Index interface. It reports whether the
// index might contain h using the in-memory fanout mapping. Returns
// false iff h's first byte falls in an empty fanout bucket.
func (idx *MemoryIndex) MayContain(h plumbing.Hash) bool {
	return idx.FanoutMapping[h.Bytes()[0]] != noMapping
}

// Contains implements the Index interface.
func (idx *MemoryIndex) Contains(h plumbing.Hash) (bool, error) {
	_, ok := idx.findHashIndex(h)
	return ok, nil
}

// FindOffset implements the Index interface.
func (idx *MemoryIndex) FindOffset(h plumbing.Hash) (int64, error) {
	fo := h.Bytes()[0]
	if len(idx.FanoutMapping) <= int(fo) {
		return 0, plumbing.ErrObjectNotFound
	}

	k := idx.FanoutMapping[fo]
	i, ok := idx.findHashIndex(h)
	if !ok {
		return 0, plumbing.ErrObjectNotFound
	}

	offset, err := idx.getOffset(k, i)
	if err != nil {
		return 0, err
	}

	// Save the offset for reverse lookup
	idx.mu.Lock()
	if idx.offsetHash == nil {
		idx.offsetHash = make(map[int64]plumbing.Hash)
	}
	idx.offsetHash[int64(offset)] = h
	idx.mu.Unlock()

	return int64(offset), nil
}

const isO64Mask = uint64(1) << 31

func (idx *MemoryIndex) getOffset(firstLevel, secondLevel int) (uint64, error) {
	offset := secondLevel << 2
	ofs := encbin.BigEndian.Uint32(idx.Offset32[firstLevel][offset : offset+4])

	if (uint64(ofs) & isO64Mask) != 0 {
		offset := 8 * (uint64(ofs) & ^isO64Mask)
		if l := uint64(len(idx.Offset64)); l < 8 || offset > l-8 {
			return 0, fmt.Errorf("%w: offset64 index out of range", ErrMalformedIdxFile)
		}
		return encbin.BigEndian.Uint64(idx.Offset64[offset : offset+8]), nil
	}

	return uint64(ofs), nil
}

// FindCRC32 implements the Index interface.
func (idx *MemoryIndex) FindCRC32(h plumbing.Hash) (uint32, error) {
	k := idx.FanoutMapping[h.Bytes()[0]]
	i, ok := idx.findHashIndex(h)
	if !ok {
		return 0, plumbing.ErrObjectNotFound
	}

	return idx.getCRC32(k, i), nil
}

func (idx *MemoryIndex) getCRC32(firstLevel, secondLevel int) uint32 {
	offset := secondLevel << 2
	return encbin.BigEndian.Uint32(idx.CRC32[firstLevel][offset : offset+4])
}

// FindHash implements the Index interface.
func (idx *MemoryIndex) FindHash(o int64) (plumbing.Hash, error) {
	var hash plumbing.Hash
	var ok bool

	idx.mu.RLock()
	if idx.offsetHash != nil {
		if hash, ok = idx.offsetHash[o]; ok {
			idx.mu.RUnlock()
			return hash, nil
		}
	}
	idx.mu.RUnlock()

	var genErr error
	idx.offsetBuildOnce.Do(func() {
		genErr = idx.genOffsetHash()
	})
	if genErr != nil {
		return plumbing.ZeroHash, genErr
	}

	idx.mu.RLock()
	hash, ok = idx.offsetHash[o]
	idx.mu.RUnlock()

	if !ok {
		return plumbing.ZeroHash, plumbing.ErrObjectNotFound
	}

	return hash, nil
}

// genOffsetHash generates the offset/hash mapping for reverse search.
func (idx *MemoryIndex) genOffsetHash() error {
	count, err := idx.Count()
	if err != nil {
		return err
	}

	offsetHash := make(map[int64]plumbing.Hash, count)

	var hash plumbing.Hash
	hash.ResetBySize(idx.objectIDSize)

	i := uint32(0)
	for firstLevel, fanoutValue := range idx.Fanout {
		mappedFirstLevel := idx.FanoutMapping[firstLevel]
		for secondLevel := uint32(0); i < fanoutValue; i++ {
			_, err = hash.Write(idx.Names[mappedFirstLevel][secondLevel*uint32(idx.idSize()):])
			if err != nil {
				return fmt.Errorf("cannot write name to hash: %w", err)
			}

			off, err := idx.getOffset(mappedFirstLevel, int(secondLevel))
			if err != nil {
				return err
			}
			offsetHash[int64(off)] = hash
			secondLevel++
		}
	}

	idx.mu.Lock()
	idx.offsetHash = offsetHash
	idx.mu.Unlock()

	return nil
}

// Count implements the Index interface.
func (idx *MemoryIndex) Count() (int64, error) {
	return int64(idx.Fanout[fanout-1]), nil
}

// Entries implements the Index interface.
func (idx *MemoryIndex) Entries() (EntryIter, error) {
	return &idxfileEntryIter{idx, 0, 0, 0}, nil
}

// EntriesWithPrefix implements the Index interface. It returns an
// iterator over entries whose hashes start with prefix. When prefix
// is empty the call is equivalent to Entries; otherwise the
// iterator visits only the fanout bucket selected by prefix[0] and
// stops as soon as the sorted-by-hash bucket walks past prefix.
//
// For a multi-byte prefix the matching entries form a contiguous
// run somewhere within the bucket; binary-search positions the
// iterator at the start of that run so the linear walk only spans
// matches. This mirrors upstream Git's for_each_prefixed_object_in_pack
// which calls bsearch_pack to position before walking forward.
func (idx *MemoryIndex) EntriesWithPrefix(prefix []byte) (EntryIter, error) {
	if len(prefix) == 0 {
		return idx.Entries()
	}
	bucket := idx.FanoutMapping[prefix[0]]
	if bucket == noMapping {
		return &idxfilePrefixIter{done: true}, nil
	}
	idSize := idx.idSize()
	names := idx.Names[bucket]
	n := len(names) / idSize

	// Find the leftmost entry whose hash is >= prefix (padded with
	// zeros to hash size). All matching entries, if any, start at
	// this position; the iterator's stop-on-first-mismatch then
	// terminates correctly once the run ends.
	target := make([]byte, idSize)
	copy(target, prefix)
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) >> 1
		slot := names[mid*idSize : (mid+1)*idSize]
		if bytes.Compare(slot, target) < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	return &idxfilePrefixIter{
		idSize:   idSize,
		prefix:   prefix,
		names:    names,
		offset32: idx.Offset32[bucket],
		crc32:    idx.CRC32[bucket],
		offset64: idx.Offset64,
		pos:      lo,
	}, nil
}

// EntriesByOffset implements the Index interface.
func (idx *MemoryIndex) EntriesByOffset() (EntryIter, error) {
	count, err := idx.Count()
	if err != nil {
		return nil, err
	}

	iter := &idxfileEntryOffsetIter{
		entries: make(entriesByOffset, count),
	}

	entries, err := idx.Entries()
	if err != nil {
		return nil, err
	}

	for pos := 0; int64(pos) < count; pos++ {
		entry, err := entries.Next()
		if err != nil {
			return nil, err
		}

		iter.entries[pos] = entry
	}

	sort.Sort(iter.entries)

	return iter, nil
}

func (idx *MemoryIndex) idSize() int {
	if idx.objectIDSize != 0 {
		return idx.objectIDSize
	}
	return crypto.SHA1.Size()
}

// EntryIter is an iterator that will return the entries in a packfile index.
type EntryIter interface {
	// Next returns the next entry in the packfile index.
	Next() (*Entry, error)
	// Close closes the iterator.
	Close() error
}

type idxfileEntryIter struct {
	idx                     *MemoryIndex
	total                   int
	firstLevel, secondLevel int
}

func (i *idxfileEntryIter) Next() (*Entry, error) {
	for {
		if i.firstLevel >= fanout {
			return nil, io.EOF
		}

		if i.total >= int(i.idx.Fanout[i.firstLevel]) {
			i.firstLevel++
			i.secondLevel = 0
			continue
		}

		mappedFirstLevel := i.idx.FanoutMapping[i.firstLevel]
		entry := new(Entry)
		entry.Hash.ResetBySize(i.idx.idSize())
		_, err := entry.Hash.Write(i.idx.Names[mappedFirstLevel][i.secondLevel*i.idx.idSize():])
		if err != nil {
			return nil, fmt.Errorf("cannot write entry hash: %w", err)
		}

		entry.Offset, err = i.idx.getOffset(mappedFirstLevel, i.secondLevel)
		if err != nil {
			return nil, err
		}
		entry.CRC32 = i.idx.getCRC32(mappedFirstLevel, i.secondLevel)

		i.secondLevel++
		i.total++

		return entry, nil
	}
}

func (i *idxfileEntryIter) Close() error {
	i.firstLevel = fanout
	return nil
}

// idxfilePrefixIter walks a single fanout bucket, yielding entries
// whose hash starts with prefix. The bucket is sorted by hash, so
// once a name is read whose first bytes do not match prefix the
// iterator stops.
//
// The iterator references the bucket's per-slot slices directly
// (names, offset32, crc32) plus the shared offset64 table, so it
// does not retain a reference to the parent MemoryIndex. This keeps
// the iterator footprint to just the cursor state and the slice
// headers it actually reads from.
//
// Lifetime: the slice headers are views into the parent
// MemoryIndex's per-bucket storage. The iterator is invalid after
// the parent Index is closed or reindexed — callers must consume
// (or Close) the iterator before discarding the Index.
type idxfilePrefixIter struct {
	idSize   int
	prefix   []byte
	names    []byte // bucket's hash bytes
	offset32 []byte // bucket's 32-bit offset table
	crc32    []byte // bucket's CRC32 table
	offset64 []byte // shared 64-bit offset overflow table
	pos      int    // entries already yielded
	done     bool
}

func (i *idxfilePrefixIter) Next() (*Entry, error) {
	if i.done {
		return nil, io.EOF
	}

	offset := i.pos * i.idSize
	if offset+i.idSize > len(i.names) {
		i.done = true
		return nil, io.EOF
	}
	hashBytes := i.names[offset : offset+i.idSize]
	if !bytes.HasPrefix(hashBytes, i.prefix) {
		// Bucket is sorted by hash, so the first mismatch ends the run.
		i.done = true
		return nil, io.EOF
	}

	entry := new(Entry)
	entry.Hash.ResetBySize(i.idSize)
	if _, err := entry.Hash.Write(hashBytes); err != nil {
		return nil, fmt.Errorf("cannot write entry hash: %w", err)
	}

	o, err := i.bucketOffset(i.pos)
	if err != nil {
		return nil, err
	}
	entry.Offset = o
	entry.CRC32 = i.bucketCRC32(i.pos)
	i.pos++
	return entry, nil
}

// bucketOffset mirrors MemoryIndex.getOffset using only the per-
// bucket Offset32/Offset64 slices the iterator holds, so callers
// do not need to retain a reference to the parent MemoryIndex.
func (i *idxfilePrefixIter) bucketOffset(pos int) (uint64, error) {
	off := pos << 2
	ofs := encbin.BigEndian.Uint32(i.offset32[off : off+4])
	if (uint64(ofs) & isO64Mask) != 0 {
		o64 := 8 * (uint64(ofs) & ^isO64Mask)
		if l := uint64(len(i.offset64)); l < 8 || o64 > l-8 {
			return 0, fmt.Errorf("%w: offset64 index out of range", ErrMalformedIdxFile)
		}
		return encbin.BigEndian.Uint64(i.offset64[o64 : o64+8]), nil
	}
	return uint64(ofs), nil
}

// bucketCRC32 mirrors MemoryIndex.getCRC32 using only the per-bucket
// CRC32 slice the iterator holds.
func (i *idxfilePrefixIter) bucketCRC32(pos int) uint32 {
	off := pos << 2
	return encbin.BigEndian.Uint32(i.crc32[off : off+4])
}

func (i *idxfilePrefixIter) Close() error {
	i.done = true
	return nil
}

// Entry is the in memory representation of an object entry in the idx file.
type Entry struct {
	Hash   plumbing.Hash
	CRC32  uint32
	Offset uint64
}

type idxfileEntryOffsetIter struct {
	entries entriesByOffset
	pos     int
}

func (i *idxfileEntryOffsetIter) Next() (*Entry, error) {
	if i.pos >= len(i.entries) {
		return nil, io.EOF
	}

	entry := i.entries[i.pos]
	i.pos++

	return entry, nil
}

func (i *idxfileEntryOffsetIter) Close() error {
	i.pos = len(i.entries) + 1
	return nil
}

type entriesByOffset []*Entry

func (o entriesByOffset) Len() int {
	return len(o)
}

func (o entriesByOffset) Less(i, j int) bool {
	return o[i].Offset < o[j].Offset
}

func (o entriesByOffset) Swap(i, j int) {
	o[i], o[j] = o[j], o[i]
}
