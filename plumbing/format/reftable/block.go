package reftable

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

	gbinary "github.com/go-git/go-git/v6/utils/binary"
)

// Block types.
const (
	blockTypeRef   = 'r'
	blockTypeLog   = 'g'
	blockTypeIndex = 'i'
)

// blockHeaderSize is the size of a block header (type byte + uint24 length).
const blockHeaderSize = 4

// blockReader provides access to records within a single reftable block.
type blockReader struct {
	blockType byte
	data      []byte // uncompressed record data (between header and restart table)
	restarts  []uint32
	headerLen int // length of the block header (including file header for first block)
}

// readBlock reads and parses a single block from data starting at the given
// offset within the full block data. fileHeaderSize should be non-zero only
// for the first block in a file (to account for the file header sharing the
// first block).
//
// For log blocks ('g'), the data is zlib-compressed. The blockLen field in the
// header gives the inflated size.
func readBlock(raw []byte, fileHeaderSize int) (*blockReader, error) {
	if len(raw) < blockHeaderSize+fileHeaderSize {
		return nil, fmt.Errorf("%w: block too small", ErrCorruptBlock)
	}

	blockType := raw[fileHeaderSize]
	blockLen := int(raw[fileHeaderSize+1])<<16 | int(raw[fileHeaderSize+2])<<8 | int(raw[fileHeaderSize+3])

	headerStart := fileHeaderSize
	br := &blockReader{
		blockType: blockType,
		headerLen: fileHeaderSize + blockHeaderSize,
	}

	var recordData []byte
	if blockType == blockTypeLog {
		// Log blocks are zlib-compressed. blockLen is the inflated size.
		// The compressed data starts right after the 4-byte block header.
		compressedStart := headerStart + blockHeaderSize
		compressedData := raw[compressedStart:]

		zr, err := zlib.NewReader(bytes.NewReader(compressedData))
		if err != nil {
			return nil, fmt.Errorf("%w: zlib init: %v", ErrCorruptBlock, err)
		}
		defer func() { _ = zr.Close() }()

		inflatedSize := blockLen - blockHeaderSize
		if inflatedSize <= 0 {
			return nil, fmt.Errorf("%w: invalid log block inflated size", ErrCorruptBlock)
		}
		inflated := make([]byte, inflatedSize)
		if _, err := io.ReadFull(zr, inflated); err != nil {
			return nil, fmt.Errorf("%w: zlib inflate: %v", ErrCorruptBlock, err)
		}

		recordData = inflated
	} else {
		// For non-log blocks, data runs from after the header up to blockLen.
		dataEnd := min(blockLen, len(raw))
		recordData = raw[headerStart+blockHeaderSize : dataEnd]
	}

	// Parse the restart table from the end of recordData.
	// Last 2 bytes: uint16 restart_count.
	if len(recordData) < 2 {
		return nil, fmt.Errorf("%w: block data too small for restart count", ErrCorruptBlock)
	}

	restartCount := int(binary.BigEndian.Uint16(recordData[len(recordData)-2:]))

	// Before the restart_count, there are restartCount * 3 bytes of uint24 offsets.
	restartTableSize := restartCount*3 + 2
	if restartTableSize > len(recordData) {
		return nil, fmt.Errorf("%w: restart table exceeds block data", ErrCorruptBlock)
	}

	restartBase := len(recordData) - restartTableSize
	restarts := make([]uint32, restartCount)
	for i := range restartCount {
		off := restartBase + i*3
		restarts[i] = uint32(recordData[off])<<16 | uint32(recordData[off+1])<<8 | uint32(recordData[off+2])
	}

	// Record data is everything before the restart table.
	br.data = recordData[:restartBase]
	br.restarts = restarts

	return br, nil
}

// seek finds the record with the given key using binary search over restart
// points, then linear scan. Returns the position within br.data where the
// record starts, or -1 if not found.
// The keyCompare function should compare the reconstructed record key with the
// target: negative if record < target, 0 if equal, positive if record > target.
//
// For ref blocks, the offset in the restart table is relative to the start of
// the file, but our data starts after the header. We need to adjust offsets.
func (br *blockReader) seek(target string) int {
	if len(br.restarts) == 0 {
		return -1
	}

	// Binary search over restart points to find the last restart <= target.
	// Restart offsets are relative to the start of the block in the file,
	// so we subtract br.headerLen to get positions within br.data.
	adjustedRestarts := make([]int, len(br.restarts))
	for i, r := range br.restarts {
		adj := max(int(r)-br.headerLen, 0)
		adjustedRestarts[i] = adj
	}

	lo, hi := 0, len(adjustedRestarts)-1
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		pos := adjustedRestarts[mid]
		if pos >= len(br.data) {
			hi = mid - 1
			continue
		}
		// At a restart point, prefix_length is 0, so the first varint is 0
		// and the name can be read directly.
		name := readKeyAtRestart(br.data, pos)
		if name <= target {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	return adjustedRestarts[lo]
}

// readKeyAtRestart reads the key at a restart point (where prefix_length == 0).
func readKeyAtRestart(data []byte, pos int) string {
	if pos >= len(data) {
		return ""
	}

	// prefix_length varint (should be 0).
	_, n := gbinary.GetVarInt(data[pos:])
	if n == 0 {
		return ""
	}
	pos += n

	// (suffix_length << 3) | value_type
	suffixType, n := gbinary.GetVarInt(data[pos:])
	if n == 0 {
		return ""
	}
	pos += n

	suffixLen := int(suffixType >> 3)
	if pos+suffixLen > len(data) {
		return ""
	}
	return string(data[pos : pos+suffixLen])
}

// iterRecords iterates all ref records in the block, calling fn for each.
// Iteration stops when fn returns false or all records are consumed.
func (br *blockReader) iterRefRecords(hashSize int, minUpdateIndex uint64, fn func(RefRecord) bool) error {
	pos := 0
	prevName := ""

	for pos < len(br.data) {
		rec, n, err := decodeRefRecord(br.data[pos:], prevName, hashSize, minUpdateIndex)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		pos += n
		prevName = rec.RefName

		if !fn(rec) {
			return nil
		}
	}

	return nil
}

// iterLogRecords iterates all log records in the block.
func (br *blockReader) iterLogRecords(hashSize int, fn func(LogRecord) bool) error {
	pos := 0
	prevKey := ""

	for pos < len(br.data) {
		rec, n, err := decodeLogRecord(br.data[pos:], prevKey, hashSize)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		pos += n

		// Reconstruct the key for prefix compression of next record.
		prevKey = logKey(rec.RefName, rec.UpdateIndex)

		if !fn(rec) {
			return nil
		}
	}

	return nil
}

// iterIndexRecords iterates all index records in the block.
func (br *blockReader) iterIndexRecords(fn func(indexRecord) bool) error {
	pos := 0
	prevKey := ""

	for pos < len(br.data) {
		rec, n, err := decodeIndexRecord(br.data[pos:], prevKey)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		pos += n
		prevKey = rec.LastKey

		if !fn(rec) {
			return nil
		}
	}

	return nil
}

// logKey constructs a log record key from refname and update_index.
func logKey(refName string, updateIndex uint64) string {
	rev := ^updateIndex
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], rev)
	return refName + "\x00" + string(buf[:])
}
