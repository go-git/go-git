package idxfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	gsync "github.com/go-git/go-git/v6/utils/sync"
)

const (
	idxHeaderSize = 8 // 4 magic + 4 version
	idxFanoutSize = 256 * 4
	off32Size     = 4
	off64Size     = 8
	revHeaderSize = 12 // 4 magic + 4 version + 4 hash function

	is64bitsMask = uint64(1) << 31
)

// ReadAtCloser is the interface required for files used by LazyIndex.
// It combines random-access reads with sequential read/seek/close.
// [billy.File] satisfies this interface.
type ReadAtCloser interface {
	io.ReaderAt
	io.ReadCloser
}

// openFileFunc opens a file for reading. Each call must return a fresh,
// independently closeable handle. The caller is responsible for closing
// the returned ReadAtCloser.
type openFileFunc func() (ReadAtCloser, error)

// LazyIndex implements the Index interface by reading directly from
// .idx and .rev files via ReadAt, without loading all data into memory.
//
// File descriptors are managed automatically via reference-counted
// shared handles: opened lazily on first use, shared across concurrent
// readers, and closed when no readers remain. This avoids holding
// descriptors open indefinitely while still sharing a single FD across
// concurrent operations.
type LazyIndex struct {
	hashSize int
	count    int

	// Section byte offsets within the idx file.
	fanoutStart int
	namesStart  int
	crcStart    int
	off32Start  int
	off64Start  int

	idx *sharedFile
	rev *sharedFile

	fanout [256]uint32 // cached from idx; small enough to keep in memory
}

var _ Index = (*LazyIndex)(nil)

// NewLazyIndex creates a LazyIndex from opener functions for .idx and
// .rev files.
//
// The openers are called to obtain file handles on demand. Each call
// must return a fresh, independently closeable handle. File descriptors
// are shared across concurrent readers and released automatically when
// idle.
func NewLazyIndex(openIdx, openRev func() (ReadAtCloser, error), packHash plumbing.Hash) (*LazyIndex, error) {
	if openIdx == nil {
		return nil, errors.New("idx opener is nil")
	}
	if openRev == nil {
		return nil, errors.New("rev opener is nil")
	}

	s := &LazyIndex{
		idx: newSharedFile(openIdx),
		rev: newSharedFile(openRev),
	}

	if err := s.init(packHash); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// init reads and validates headers, caches the fanout table and
// computes section offsets. It acquires file handles through the
// sharedFile so the grace period keeps them warm for the first real
// operation.
func (s *LazyIndex) init(packHash plumbing.Hash) error {
	idxRA, err := s.idx.acquire()
	if err != nil {
		return fmt.Errorf("cannot open idx: %w", err)
	}
	defer s.idx.release()

	revRA, err := s.rev.acquire()
	if err != nil {
		return fmt.Errorf("cannot open rev: %w", err)
	}
	defer s.rev.release()

	var hdr [idxHeaderSize]byte
	if _, err := idxRA.ReadAt(hdr[:], 0); err != nil {
		return fmt.Errorf("cannot read idx header: %w", err)
	}
	if !bytes.Equal(hdr[:4], idxHeader) {
		return fmt.Errorf("%w: %s", ErrMalformedIdxFile, "header mismatch")
	}

	v := binary.BigEndian.Uint32(hdr[4:])
	if v != VersionSupported {
		return ErrUnsupportedVersion
	}

	var revHdr [revHeaderSize]byte
	if _, err := revRA.ReadAt(revHdr[:], 0); err != nil {
		return fmt.Errorf("cannot read rev header: %w", err)
	}
	if !bytes.Equal(revHdr[:4], []byte{'R', 'I', 'D', 'X'}) {
		return fmt.Errorf("%w: rev file magic mismatch", ErrMalformedIdxFile)
	}
	if v := binary.BigEndian.Uint32(revHdr[4:]); v != 1 {
		return fmt.Errorf("%w: unsupported rev file version %d", ErrMalformedIdxFile, v)
	}

	s.fanoutStart = idxHeaderSize
	var fanoutBuf [idxFanoutSize]byte
	if _, err := idxRA.ReadAt(fanoutBuf[:], int64(s.fanoutStart)); err != nil {
		return fmt.Errorf("cannot read idx fanout: %w", err)
	}

	for i := range 256 {
		s.fanout[i] = binary.BigEndian.Uint32(fanoutBuf[i*4:])
		if i > 0 && s.fanout[i] < s.fanout[i-1] {
			return fmt.Errorf("%w: fanout table is not monotonically non-decreasing at entry %d",
				ErrMalformedIdxFile, i)
		}
	}

	s.count = int(s.fanout[255])

	s.hashSize = packHash.Size()
	s.namesStart = s.fanoutStart + idxFanoutSize
	s.crcStart = s.namesStart + (s.count * s.hashSize)
	s.off32Start = s.crcStart + (s.count * 4)
	s.off64Start = s.off32Start + (s.count * off32Size)

	// Count 64-bit offset entries by scanning the 32-bit offset table
	// for entries with the MSB set.
	n64, err := s.count64bitOffsets(idxRA)
	if err != nil {
		return err
	}

	// The pack checksum sits right after the 64-bit offset table.
	packBuf := make([]byte, s.hashSize)
	packHashOff := int64(s.off64Start + n64*off64Size)
	if _, err := idxRA.ReadAt(packBuf, packHashOff); err != nil {
		return fmt.Errorf("cannot read pack checksum: %w", err)
	}

	if packHash.Compare(packBuf) != 0 {
		var got plumbing.Hash
		got.ResetBySize(s.hashSize)
		_, _ = got.Write(packBuf)
		return fmt.Errorf("%w: packfile mismatch: got %q instead of %q",
			ErrMalformedIdxFile, got.String(), packHash.String())
	}

	return nil
}

// Contains reports whether the given hash exists in the index by
// binary-searching the idx names table.
func (s *LazyIndex) Contains(h plumbing.Hash) (bool, error) {
	idx, err := s.idx.acquire()
	if err != nil {
		return false, err
	}
	defer s.idx.release()

	_, found, err := s.findHashPos(idx, h)
	return found, err
}

// FindOffset returns the packfile offset for the object with the given hash.
// It returns plumbing.ErrObjectNotFound if the hash is not in the index.
func (s *LazyIndex) FindOffset(h plumbing.Hash) (int64, error) {
	idx, err := s.idx.acquire()
	if err != nil {
		return 0, err
	}
	defer s.idx.release()

	pos, found, err := s.findHashPos(idx, h)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, plumbing.ErrObjectNotFound
	}

	off, err := s.offset(idx, pos)
	if err != nil {
		return 0, err
	}

	return int64(off), nil
}

// FindCRC32 returns the CRC32 checksum of the object with the given hash.
// It returns plumbing.ErrObjectNotFound if the hash is not in the index.
func (s *LazyIndex) FindCRC32(h plumbing.Hash) (uint32, error) {
	idx, err := s.idx.acquire()
	if err != nil {
		return 0, err
	}
	defer s.idx.release()

	pos, found, err := s.findHashPos(idx, h)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, plumbing.ErrObjectNotFound
	}

	return s.crc32(idx, pos)
}

// FindHash returns the object hash stored at the given packfile offset
// by binary-searching the .rev reverse index.
// It returns plumbing.ErrObjectNotFound if no object exists at that offset.
func (s *LazyIndex) FindHash(o int64) (plumbing.Hash, error) {
	idx, err := s.idx.acquire()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer s.idx.release()

	rev, err := s.rev.acquire()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer s.rev.release()

	return s.findHashViaRev(idx, rev, o)
}

// Count returns the total number of objects in the index.
func (s *LazyIndex) Count() (int64, error) {
	return int64(s.count), nil
}

// Entries returns an iterator over all index entries in hash order.
// The caller must call Close on the returned iterator to release the
// underlying file reference.
func (s *LazyIndex) Entries() (EntryIter, error) {
	idx, err := s.idx.acquire()
	if err != nil {
		return nil, err
	}
	return &scannerEntryIter{s: s, idx: idx}, nil
}

// EntriesByOffset returns an iterator over all index entries sorted by
// their packfile offset. It reads positions from the .rev file on each
// call to Next, avoiding any up-front allocation or sorting.
//
// The caller must call Close on the returned iterator to release the
// underlying file references.
func (s *LazyIndex) EntriesByOffset() (EntryIter, error) {
	idx, err := s.idx.acquire()
	if err != nil {
		return nil, err
	}
	rev, err := s.rev.acquire()
	if err != nil {
		s.idx.release()
		return nil, err
	}
	return &revEntryIter{s: s, idx: idx, rev: rev}, nil
}

// Close releases the underlying shared file handles, preventing future
// operations. If there are active readers they will finish normally;
// the file descriptors close when the last reader is done.
func (s *LazyIndex) Close() error {
	return errors.Join(s.idx.Close(), s.rev.Close())
}

// --- internal helpers; all take an io.ReaderAt so the caller controls
//     the acquire/release lifecycle. ---

// findHashPos binary-searches the names table for h, returning the flat
// position (0..count-1) if found.
func (s *LazyIndex) findHashPos(idx io.ReaderAt, h plumbing.Hash) (int, bool, error) {
	if h.Size() != s.hashSize {
		return 0, false, fmt.Errorf("hash size mismatch: %d %d", h.Size(), s.hashSize)
	}
	first := int(h.Bytes()[0])
	var lo int
	if first > 0 {
		lo = int(s.fanout[first-1])
	}
	hi := int(s.fanout[first])
	if lo >= hi {
		return 0, false, nil
	}

	target := h.Bytes()[:s.hashSize]
	var arr [32]byte
	buf := arr[:s.hashSize]

	for lo < hi {
		mid := (lo + hi) >> 1
		nameOff := int64(s.namesStart + mid*s.hashSize)
		if _, err := idx.ReadAt(buf, nameOff); err != nil {
			return 0, false, fmt.Errorf("read name at pos %d: %w", mid, err)
		}

		cmp := bytes.Compare(target, buf)
		switch {
		case cmp < 0:
			hi = mid
		case cmp > 0:
			lo = mid + 1
		default:
			return mid, true, nil
		}
	}
	return 0, false, nil
}

// offset returns the pack offset for the object at position pos.
func (s *LazyIndex) offset(idx io.ReaderAt, pos int) (uint64, error) {
	var buf [off32Size]byte
	off := int64(s.off32Start + pos*off32Size)
	if _, err := idx.ReadAt(buf[:], off); err != nil {
		return 0, fmt.Errorf("%w: cannot read offset32: %v", ErrMalformedIdxFile, err)
	}

	off32 := binary.BigEndian.Uint32(buf[:])
	if uint64(off32)&is64bitsMask != 0 {
		loIndex := int(uint64(off32) & ^is64bitsMask)
		var buf64 [off64Size]byte
		off64Pos := int64(s.off64Start + loIndex*off64Size)
		if _, err := idx.ReadAt(buf64[:], off64Pos); err != nil {
			return 0, fmt.Errorf("%w: cannot read offset64: %v", ErrMalformedIdxFile, err)
		}
		return binary.BigEndian.Uint64(buf64[:]), nil
	}

	return uint64(off32), nil
}

// count64bitOffsets scans the 32-bit offset table and returns the number
// of entries whose MSB is set (i.e. that use the 64-bit overflow table).
func (s *LazyIndex) count64bitOffsets(idx io.ReaderAt) (int, error) {
	bufp := gsync.GetByteSlice()
	defer gsync.PutByteSlice(bufp)

	buf := *bufp
	// Round down to a multiple of off32Size so we always read whole entries.
	buf = buf[:len(buf)&^(off32Size-1)]

	var n int
	remaining := s.count
	pos := int64(s.off32Start)

	for remaining > 0 {
		chunk := min(remaining*off32Size, len(buf))

		if _, err := idx.ReadAt(buf[:chunk], pos); err != nil {
			return 0, fmt.Errorf("%w: cannot read offset32 table: %v", ErrMalformedIdxFile, err)
		}

		for i := 0; i < chunk; i += off32Size {
			if binary.BigEndian.Uint32(buf[i:])&uint32(is64bitsMask) != 0 {
				n++
			}
		}

		pos += int64(chunk)
		remaining -= chunk / off32Size
	}
	return n, nil
}

// crc32 returns the CRC32 for the object at position pos.
func (s *LazyIndex) crc32(idx io.ReaderAt, pos int) (uint32, error) {
	var buf [4]byte
	off := int64(s.crcStart + pos*4)
	if _, err := idx.ReadAt(buf[:], off); err != nil {
		return 0, fmt.Errorf("%w: cannot read CRC32: %v", ErrMalformedIdxFile, err)
	}
	return binary.BigEndian.Uint32(buf[:]), nil
}

// hashAtPos reads the hash at the given flat position.
func (s *LazyIndex) hashAtPos(idx io.ReaderAt, pos int) (plumbing.Hash, error) {
	var arr [32]byte
	buf := arr[:s.hashSize]
	off := int64(s.namesStart + pos*s.hashSize)
	if _, err := idx.ReadAt(buf, off); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("read name at pos %d: %w", pos, err)
	}

	var h plumbing.Hash
	h.ResetBySize(s.hashSize)
	_, _ = h.Write(buf)
	return h, nil
}

func (s *LazyIndex) findHashViaRev(idx, rev io.ReaderAt, want int64) (plumbing.Hash, error) {
	lo, hi := 0, s.count
	var buf [4]byte

	for lo < hi {
		mid := (lo + hi) >> 1
		revOff := int64(revHeaderSize + mid*4)
		if _, err := rev.ReadAt(buf[:], revOff); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("read rev entry: %w", err)
		}

		idxPos := int(binary.BigEndian.Uint32(buf[:]))
		if idxPos < 0 || idxPos >= s.count {
			return plumbing.ZeroHash, fmt.Errorf("%w: rev entry %d out of range (count %d)",
				ErrMalformedIdxFile, idxPos, s.count)
		}
		got, err := s.offset(idx, idxPos)
		if err != nil {
			return plumbing.ZeroHash, err
		}

		switch {
		case int64(got) < want:
			lo = mid + 1
		case int64(got) > want:
			hi = mid
		default:
			return s.hashAtPos(idx, idxPos)
		}
	}
	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

// entryAt reads a complete entry at the given flat position.
func (s *LazyIndex) entryAt(idx io.ReaderAt, pos int) (*Entry, error) {
	h, err := s.hashAtPos(idx, pos)
	if err != nil {
		return nil, err
	}
	off, err := s.offset(idx, pos)
	if err != nil {
		return nil, err
	}
	crc, err := s.crc32(idx, pos)
	if err != nil {
		return nil, err
	}
	return &Entry{Hash: h, Offset: off, CRC32: crc}, nil
}

// scannerEntryIter iterates over entries in hash order.
// It holds an acquired reference to the idx sharedFile which is
// released when Close is called.
type scannerEntryIter struct {
	s   *LazyIndex
	idx io.ReaderAt // acquired from s.idx
	pos int
}

func (it *scannerEntryIter) Next() (*Entry, error) {
	if it.idx == nil {
		return nil, errSharedFileClosed
	}
	if it.pos >= it.s.count {
		return nil, io.EOF
	}

	e, err := it.s.entryAt(it.idx, it.pos)
	if err != nil {
		return nil, err
	}
	it.pos++
	return e, nil
}

func (it *scannerEntryIter) Close() error {
	it.pos = it.s.count
	if it.idx != nil {
		it.s.idx.release()
		it.idx = nil
	}
	return nil
}

// revEntryIter iterates over entries in packfile-offset order by
// walking the .rev file sequentially. It holds acquired references to
// both the idx and rev sharedFiles, released on Close.
type revEntryIter struct {
	s   *LazyIndex
	idx io.ReaderAt
	rev io.ReaderAt
	pos int
}

func (it *revEntryIter) Next() (*Entry, error) {
	if it.idx == nil || it.rev == nil {
		return nil, errSharedFileClosed
	}
	if it.pos >= it.s.count {
		return nil, io.EOF
	}

	var buf [4]byte
	revOff := int64(revHeaderSize + it.pos*4)
	if _, err := it.rev.ReadAt(buf[:], revOff); err != nil {
		return nil, fmt.Errorf("read rev entry at %d: %w", it.pos, err)
	}

	idxPos := int(binary.BigEndian.Uint32(buf[:]))
	if idxPos < 0 || idxPos >= it.s.count {
		return nil, fmt.Errorf("%w: rev entry %d out of range (count %d)",
			ErrMalformedIdxFile, idxPos, it.s.count)
	}

	e, err := it.s.entryAt(it.idx, idxPos)
	if err != nil {
		return nil, err
	}

	it.pos++
	return e, nil
}

func (it *revEntryIter) Close() error {
	it.pos = it.s.count
	if it.idx != nil {
		it.s.idx.release()
		it.idx = nil
	}
	if it.rev != nil {
		it.s.rev.release()
		it.rev = nil
	}
	return nil
}
