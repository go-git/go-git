package idxfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/go-git/go-git/v6/plumbing"
)

const (
	idxHeaderSize = 8 // 4 magic + 4 version
	idxFanoutSize = 256 * 4
	off32Size     = 4
	off64Size     = 8
	revHeaderSize = 12 // 4 magic + 4 version + 4 hash function

	is64bitsMask = uint64(1) << 31
)

// LazyIndex implements the Index interface by reading directly from
// .idx and .rev files via ReadAt, without loading all data into memory.
type LazyIndex struct {
	hashSize int
	count    int

	// Section byte offsets within the idx file.
	fanoutStart int
	namesStart  int
	crcStart    int
	off32Start  int
	off64Start  int

	idx readAtCloser
	rev readAtCloser

	fanout [256]uint32 // cached from idx; small enough to keep in memory
}

var _ Index = (*LazyIndex)(nil)

// NewLazyIndex creates an LazyIndex from .idx and .rev file handles.
//
// The respective file descriptors for .idx and .rev will be kept opened
// until Close() is called.
func NewLazyIndex(idx, rev readAtCloser, packHash plumbing.Hash) (*LazyIndex, error) {
	if isNilReader(idx) {
		return nil, errors.New("idx is nil")
	}
	if isNilReader(rev) {
		return nil, errors.New("rev is nil")
	}

	s := &LazyIndex{
		idx: idx,
		rev: rev,
	}

	if err := s.init(packHash); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *LazyIndex) init(packHash plumbing.Hash) error {
	var hdr [idxHeaderSize]byte
	if _, err := s.idx.ReadAt(hdr[:], 0); err != nil {
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
	if _, err := s.rev.ReadAt(revHdr[:], 0); err != nil {
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
	if _, err := s.idx.ReadAt(fanoutBuf[:], int64(s.fanoutStart)); err != nil {
		return fmt.Errorf("cannot read idx fanout: %w", err)
	}

	for i := range 256 {
		s.fanout[i] = binary.BigEndian.Uint32(fanoutBuf[i*4:])
	}
	s.count = int(s.fanout[255])

	s.hashSize = packHash.Size()
	s.namesStart = s.fanoutStart + idxFanoutSize
	s.crcStart = s.namesStart + (s.count * s.hashSize)
	s.off32Start = s.crcStart + (s.count * 4)
	s.off64Start = s.off32Start + (s.count * off32Size)

	packBuf := make([]byte, s.hashSize)
	_, err := s.idx.Seek(-(int64(s.hashSize) * 2), io.SeekEnd)
	if err != nil {
		return err
	}

	if _, err := io.ReadFull(s.idx, packBuf); err != nil {
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
	_, found, err := s.findHashPos(h)
	if err != nil {
		return false, err
	}

	return found, nil
}

// FindOffset returns the packfile offset for the object with the given hash.
// It returns plumbing.ErrObjectNotFound if the hash is not in the index.
func (s *LazyIndex) FindOffset(h plumbing.Hash) (int64, error) {
	pos, found, err := s.findHashPos(h)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, plumbing.ErrObjectNotFound
	}

	off, err := s.offset(pos)
	if err != nil {
		return 0, err
	}

	return int64(off), nil
}

// FindCRC32 returns the CRC32 checksum of the object with the given hash.
// It returns plumbing.ErrObjectNotFound if the hash is not in the index.
func (s *LazyIndex) FindCRC32(h plumbing.Hash) (uint32, error) {
	pos, found, err := s.findHashPos(h)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, plumbing.ErrObjectNotFound
	}

	return s.crc32(pos)
}

// FindHash returns the object hash stored at the given packfile offset
// by binary-searching the .rev reverse index.
// It returns plumbing.ErrObjectNotFound if no object exists at that offset.
func (s *LazyIndex) FindHash(o int64) (plumbing.Hash, error) {
	return s.findHashViaRev(o)
}

// Count returns the total number of objects in the index.
func (s *LazyIndex) Count() (int64, error) {
	return int64(s.count), nil
}

// Entries returns an iterator over all index entries in hash order.
func (s *LazyIndex) Entries() (EntryIter, error) {
	return &scannerEntryIter{s: s, pos: 0}, nil
}

// EntriesByOffset returns an iterator over all index entries sorted by
// their packfile offset.
func (s *LazyIndex) EntriesByOffset() (EntryIter, error) {
	count := s.count
	entries := make(entriesByOffset, count)

	iter := &scannerEntryIter{s: s}
	for i := range count {
		e, err := iter.Next()
		if err != nil {
			return nil, err
		}
		entries[i] = e
	}

	sort.Sort(entries)
	return &idxfileEntryOffsetIter{entries: entries}, nil
}

// Close releases the underlying file handles. It is safe to call
// multiple times; subsequent calls return nil.
func (s *LazyIndex) Close() error {
	var errs []error
	if s.idx != nil {
		errs = append(errs, s.idx.Close())
		s.idx = nil
	}
	if s.rev != nil {
		errs = append(errs, s.rev.Close())
		s.rev = nil
	}

	return errors.Join(errs...)
}

// findHashPos binary-searches the names table for h, returning the flat
// position (0..count-1) if found.
func (s *LazyIndex) findHashPos(h plumbing.Hash) (int, bool, error) {
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
		if _, err := s.idx.ReadAt(buf, nameOff); err != nil {
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
func (s *LazyIndex) offset(pos int) (uint64, error) {
	var buf [off32Size]byte
	off := int64(s.off32Start + pos*off32Size)
	if _, err := s.idx.ReadAt(buf[:], off); err != nil {
		return 0, fmt.Errorf("%w: cannot read offset32: %v", ErrMalformedIdxFile, err)
	}

	off32 := binary.BigEndian.Uint32(buf[:])
	if uint64(off32)&is64bitsMask != 0 {
		loIndex := int(uint64(off32) & ^is64bitsMask)
		var buf64 [off64Size]byte
		off64Pos := int64(s.off64Start + loIndex*off64Size)
		if _, err := s.idx.ReadAt(buf64[:], off64Pos); err != nil {
			return 0, fmt.Errorf("%w: cannot read offset64: %v", ErrMalformedIdxFile, err)
		}
		return binary.BigEndian.Uint64(buf64[:]), nil
	}

	return uint64(off32), nil
}

// crc32 returns the CRC32 for the object at position pos.
func (s *LazyIndex) crc32(pos int) (uint32, error) {
	var buf [4]byte
	off := int64(s.crcStart + pos*4)
	if _, err := s.idx.ReadAt(buf[:], off); err != nil {
		return 0, fmt.Errorf("%w: cannot read CRC32: %v", ErrMalformedIdxFile, err)
	}
	return binary.BigEndian.Uint32(buf[:]), nil
}

// hashAtPos reads the hash at the given flat position.
func (s *LazyIndex) hashAtPos(pos int) (plumbing.Hash, error) {
	var arr [32]byte
	buf := arr[:s.hashSize]
	off := int64(s.namesStart + pos*s.hashSize)
	if _, err := s.idx.ReadAt(buf, off); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("read name at pos %d: %w", pos, err)
	}

	var h plumbing.Hash
	h.ResetBySize(s.hashSize)
	_, _ = h.Write(buf)
	return h, nil
}

func (s *LazyIndex) findHashViaRev(want int64) (plumbing.Hash, error) {
	lo, hi := 0, s.count
	var buf [4]byte

	for lo < hi {
		mid := (lo + hi) >> 1
		revOff := int64(revHeaderSize + mid*4)
		if _, err := s.rev.ReadAt(buf[:], revOff); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("read rev entry: %w", err)
		}

		idxPos := int(binary.BigEndian.Uint32(buf[:]))
		got, err := s.offset(idxPos)
		if err != nil {
			return plumbing.ZeroHash, err
		}

		switch {
		case int64(got) < want:
			lo = mid + 1
		case int64(got) > want:
			hi = mid
		default:
			return s.hashAtPos(idxPos)
		}
	}
	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

// scannerEntryIter iterates over entries in hash order using ReadAt.
type scannerEntryIter struct {
	s   *LazyIndex
	pos int
}

func (it *scannerEntryIter) Next() (*Entry, error) {
	if it.pos >= it.s.count {
		return nil, io.EOF
	}

	h, err := it.s.hashAtPos(it.pos)
	if err != nil {
		return nil, err
	}

	off, err := it.s.offset(it.pos)
	if err != nil {
		return nil, err
	}

	crc, err := it.s.crc32(it.pos)
	if err != nil {
		return nil, err
	}

	e := &Entry{
		Hash:   h,
		Offset: off,
		CRC32:  crc,
	}
	it.pos++
	return e, nil
}

func (it *scannerEntryIter) Close() error {
	it.pos = it.s.count
	return nil
}

// isNilReader reports whether r is nil or wraps a nil pointer.
// It catches both untyped nils and typed interface values holding a nil
// concrete pointer (e.g. (*os.File)(nil) assigned to an interface).
func isNilReader(r readAtCloser) (isNil bool) {
	if r == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			isNil = true
		}
	}()
	// A zero-length ReadAt at offset 0 will panic (or return an error)
	// if the underlying value is a nil pointer.
	_, _ = r.ReadAt([]byte{}, 0)
	return false
}

type readAtCloser interface {
	io.ReaderAt
	io.ReadSeekCloser
}
