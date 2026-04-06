package reftable

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

var magic = [4]byte{'R', 'E', 'F', 'T'}

const (
	headerSizeV1 = 24
	headerSizeV2 = 28
	footerSizeV1 = 68
	footerSizeV2 = 72

	versionV1 = 1
	versionV2 = 2
)

// footer holds the parsed footer of a reftable file.
type footer struct {
	version        int
	blockSize      uint32
	minUpdateIndex uint64
	maxUpdateIndex uint64
	refIndexPos    uint64
	objPos         uint64
	objIDLen       int
	objIndexPos    uint64
	logPos         uint64
	logIndexPos    uint64
	hashID         uint32 // 0 for v1 (implicit SHA-1)
}

// Table reads a single reftable file.
type Table struct {
	r        io.ReaderAt
	size     int64
	footer   footer
	hashSize int

	// Cached full file data for simplicity. For very large tables,
	// this could be replaced with on-demand block reads.
	data []byte
}

// OpenTable opens a reftable from an io.ReaderAt with the given size.
func OpenTable(r io.ReaderAt, size int64) (*Table, error) {
	t := &Table{r: r, size: size}

	// Read the entire file into memory. For typical reftable sizes
	// (kilobytes to low megabytes) this is efficient.
	t.data = make([]byte, size)
	if _, err := r.ReadAt(t.data, 0); err != nil && err != io.EOF {
		return nil, fmt.Errorf("reftable: reading file: %w", err)
	}

	if err := t.readFooter(); err != nil {
		return nil, err
	}

	return t, nil
}

func (t *Table) readFooter() error {
	// Try v1 footer size first, then v2.
	footerSize := footerSizeV1
	if t.size < int64(footerSize) {
		return fmt.Errorf("%w: file too small for footer", ErrInvalidReftable)
	}

	footerData := t.data[t.size-int64(footerSize):]

	// Check magic.
	if footerData[0] != magic[0] || footerData[1] != magic[1] ||
		footerData[2] != magic[2] || footerData[3] != magic[3] {
		// Might be v2 (4 bytes longer).
		footerSize = footerSizeV2
		if t.size < int64(footerSize) {
			return ErrBadMagic
		}
		footerData = t.data[t.size-int64(footerSize):]
		if footerData[0] != magic[0] || footerData[1] != magic[1] ||
			footerData[2] != magic[2] || footerData[3] != magic[3] {
			return ErrBadMagic
		}
	}

	version := int(footerData[4])
	switch version {
	case versionV1:
		footerSize = footerSizeV1
		footerData = t.data[t.size-int64(footerSize):]
	case versionV2:
		footerSize = footerSizeV2
		footerData = t.data[t.size-int64(footerSize):]
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, version)
	}

	// Verify CRC-32. The CRC covers all footer bytes except the last 4.
	crcData := footerData[:footerSize-4]
	expectedCRC := binary.BigEndian.Uint32(footerData[footerSize-4:])
	actualCRC := crc32.ChecksumIEEE(crcData)
	if expectedCRC != actualCRC {
		return fmt.Errorf("%w: expected %08x, got %08x", ErrBadCRC, expectedCRC, actualCRC)
	}

	pos := 5 // after magic + version byte
	t.footer.version = version
	t.footer.blockSize = uint32(footerData[pos])<<16 | uint32(footerData[pos+1])<<8 | uint32(footerData[pos+2])
	pos += 3

	t.footer.minUpdateIndex = binary.BigEndian.Uint64(footerData[pos:])
	pos += 8
	t.footer.maxUpdateIndex = binary.BigEndian.Uint64(footerData[pos:])
	pos += 8

	if version == versionV2 {
		t.footer.hashID = binary.BigEndian.Uint32(footerData[pos:])
		pos += 4
	}

	t.footer.refIndexPos = binary.BigEndian.Uint64(footerData[pos:])
	pos += 8

	objData := binary.BigEndian.Uint64(footerData[pos:])
	t.footer.objPos = objData >> 5
	t.footer.objIDLen = int(objData & 0x1f)
	pos += 8

	t.footer.objIndexPos = binary.BigEndian.Uint64(footerData[pos:])
	pos += 8
	t.footer.logPos = binary.BigEndian.Uint64(footerData[pos:])
	pos += 8
	t.footer.logIndexPos = binary.BigEndian.Uint64(footerData[pos:])

	// Determine hash size.
	if version == versionV2 {
		switch t.footer.hashID {
		case 0x73323536: // "s256"
			t.hashSize = 32
		default: // "sha1" or 0
			t.hashSize = 20
		}
	} else {
		t.hashSize = 20
	}

	// Verify the file header matches the footer.
	if len(t.data) < 5 {
		return fmt.Errorf("%w: file too small for header", ErrInvalidReftable)
	}
	if t.data[0] != magic[0] || t.data[1] != magic[1] ||
		t.data[2] != magic[2] || t.data[3] != magic[3] {
		return ErrBadMagic
	}
	if int(t.data[4]) != version {
		return fmt.Errorf("%w: header version %d != footer version %d", ErrInvalidReftable, t.data[4], version)
	}

	return nil
}

// headerSize returns the file header size for this table's version.
func (t *Table) headerSize() int {
	if t.footer.version == versionV2 {
		return headerSizeV2
	}
	return headerSizeV1
}

// footerSize returns the footer size for this table's version.
func (t *Table) footerSize() int {
	if t.footer.version == versionV2 {
		return footerSizeV2
	}
	return footerSizeV1
}

// refBlockEndPos returns the file offset where ref blocks end.
func (t *Table) refBlockEndPos() int64 {
	switch {
	case t.footer.refIndexPos > 0:
		return int64(t.footer.refIndexPos)
	case t.footer.objPos > 0:
		return int64(t.footer.objPos)
	case t.footer.logPos > 0:
		return int64(t.footer.logPos)
	default:
		return t.size - int64(t.footerSize())
	}
}

// blockAt reads a block starting at the given file offset.
func (t *Table) blockAt(offset int64, fileHeaderSize int) (*blockReader, error) {
	if offset >= t.size-int64(t.footerSize()) {
		return nil, fmt.Errorf("%w: block offset %d beyond data area", ErrCorruptBlock, offset)
	}

	end := t.size - int64(t.footerSize())
	if t.footer.blockSize > 0 {
		// Aligned blocks: block ends at the next block boundary.
		blockEnd := offset + int64(t.footer.blockSize)
		if blockEnd < end {
			end = blockEnd
		}
	}

	raw := t.data[offset:end]
	return readBlock(raw, fileHeaderSize)
}

// Ref looks up a single reference by name. Returns nil if not found.
func (t *Table) Ref(name string) (*RefRecord, error) {
	// If there's a ref index, use it.
	if t.footer.refIndexPos > 0 {
		return t.seekRefViaIndex(name)
	}

	// Otherwise, scan all ref blocks linearly.
	return t.seekRefLinear(name)
}

func (t *Table) seekRefViaIndex(name string) (*RefRecord, error) {
	// Read the index block.
	br, err := t.blockAt(int64(t.footer.refIndexPos), 0)
	if err != nil {
		return nil, err
	}

	// Multi-level index: keep traversing until we reach a ref block.
	for br.blockType == blockTypeIndex {
		var targetPos uint64
		found := false

		err = br.iterIndexRecords(func(rec indexRecord) bool {
			if name <= rec.LastKey {
				targetPos = rec.BlockPosition
				found = true
				return false
			}
			targetPos = rec.BlockPosition
			return true
		})
		if err != nil {
			return nil, err
		}
		if !found && targetPos == 0 {
			return nil, nil // not found
		}

		headerSize := 0
		if targetPos == 0 {
			headerSize = t.headerSize()
		}
		br, err = t.blockAt(int64(targetPos), headerSize)
		if err != nil {
			return nil, err
		}
	}

	return t.searchRefBlock(br, name)
}

func (t *Table) seekRefLinear(name string) (*RefRecord, error) {
	offset := int64(0)
	headerSize := t.headerSize()
	endPos := t.refBlockEndPos()

	for offset < endPos {
		br, err := t.blockAt(offset, headerSize)
		if err != nil {
			return nil, err
		}
		if br.blockType != blockTypeRef {
			break
		}

		rec, err := t.searchRefBlock(br, name)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			return rec, nil
		}

		// Advance to next block.
		if t.footer.blockSize > 0 {
			offset += int64(t.footer.blockSize)
		} else {
			// Unaligned: we'd need to track actual block size.
			// For now, this shouldn't happen with ref blocks (they're always aligned when blockSize > 0).
			break
		}
		headerSize = 0 // only first block has file header
	}

	return nil, nil
}

func (t *Table) searchRefBlock(br *blockReader, name string) (*RefRecord, error) {
	var found *RefRecord

	// Use binary search via restart points, then linear scan.
	startPos := max(br.seek(name, t.headerSize()), 0)

	pos := startPos
	prevName := ""

	// If startPos > 0, we need to find the previous name for prefix decompression.
	// Walk from the beginning of the block to reconstruct context.
	if startPos > 0 {
		p := 0
		for p < startPos && p < len(br.data) {
			rec, n, err := decodeRefRecord(br.data[p:], prevName, t.hashSize, t.footer.minUpdateIndex)
			if err != nil || n == 0 {
				break
			}
			prevName = rec.RefName
			p += n
		}
	}

	for pos < len(br.data) {
		rec, n, err := decodeRefRecord(br.data[pos:], prevName, t.hashSize, t.footer.minUpdateIndex)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			break
		}
		pos += n
		prevName = rec.RefName

		if rec.RefName == name {
			found = &rec
			break
		}
		if rec.RefName > name {
			break
		}
	}

	return found, nil
}

// IterRefs iterates all ref records in the table.
func (t *Table) IterRefs(fn func(RefRecord) bool) error {
	offset := int64(0)
	headerSize := t.headerSize()
	endPos := t.refBlockEndPos()

	for offset < endPos {
		br, err := t.blockAt(offset, headerSize)
		if err != nil {
			return err
		}
		if br.blockType != blockTypeRef {
			break
		}

		stop := false
		err = br.iterRefRecords(t.hashSize, t.footer.minUpdateIndex, func(rec RefRecord) bool {
			if !fn(rec) {
				stop = true
				return false
			}
			return true
		})
		if err != nil {
			return err
		}
		if stop {
			return nil
		}

		if t.footer.blockSize > 0 {
			offset += int64(t.footer.blockSize)
		} else {
			break
		}
		headerSize = 0
	}

	return nil
}

// IterLogs iterates all log records in the table.
func (t *Table) IterLogs(fn func(LogRecord) bool) error {
	if t.footer.logPos == 0 {
		return nil // no log blocks
	}

	// Log blocks start at logPos. They are not aligned (variable size).
	// We need to decompress each one and walk through them.
	// For simplicity, read the log section as one chunk.
	logStart := int64(t.footer.logPos)
	logEnd := t.size - int64(t.footerSize())
	if t.footer.logIndexPos > 0 {
		logEnd = int64(t.footer.logIndexPos)
	}

	// Read the first log block. Log blocks are zlib-compressed with
	// variable size, so advancing to subsequent blocks requires tracking
	// the zlib reader's consumed byte count. For now we read one block
	// which covers the common single-block case.
	if logStart+blockHeaderSize > logEnd {
		return nil
	}

	raw := t.data[logStart:logEnd]
	br, err := readBlock(raw, 0)
	if err != nil {
		return err
	}
	if br.blockType != blockTypeLog {
		return nil
	}

	return br.iterLogRecords(t.hashSize, func(rec LogRecord) bool {
		return fn(rec)
	})
}

// LogsFor returns all log records for the given reference name, newest first.
func (t *Table) LogsFor(name string) ([]LogRecord, error) {
	var records []LogRecord

	err := t.IterLogs(func(rec LogRecord) bool {
		if rec.RefName == name {
			records = append(records, rec)
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	return records, nil
}

// MinUpdateIndex returns the minimum update index of the table.
func (t *Table) MinUpdateIndex() uint64 {
	return t.footer.minUpdateIndex
}

// MaxUpdateIndex returns the maximum update index of the table.
func (t *Table) MaxUpdateIndex() uint64 {
	return t.footer.maxUpdateIndex
}
