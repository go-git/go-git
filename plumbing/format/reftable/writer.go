package reftable

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"sort"

	gbinary "github.com/go-git/go-git/v6/utils/binary"
)

const (
	defaultBlockSize   = 4096
	defaultRestartFreq = 16 // restart point every N records
)

// WriterOptions configures the reftable writer.
type WriterOptions struct {
	BlockSize      uint32 // 0 means default (4096)
	MinUpdateIndex uint64
	MaxUpdateIndex uint64
	HashSize       int // 20 for SHA-1, 32 for SHA-256; 0 defaults to 20
}

// Writer writes a single reftable file.
type Writer struct {
	w             io.Writer
	opts          WriterOptions
	blockSize     int
	hashSize      int
	restartFreq   int
	headerWritten bool
	totalWritten  int64

	// Collected records to write.
	refs []RefRecord
	logs []LogRecord
}

// NewWriter creates a new reftable writer.
func NewWriter(w io.Writer, opts WriterOptions) *Writer {
	bs := int(opts.BlockSize)
	if bs == 0 {
		bs = defaultBlockSize
	}
	hs := opts.HashSize
	if hs == 0 {
		hs = 20
	}
	return &Writer{
		w:           w,
		opts:        opts,
		blockSize:   bs,
		hashSize:    hs,
		restartFreq: defaultRestartFreq,
	}
}

// AddRef adds a ref record to be written. Refs must be added in
// lexicographic order by name, or will be sorted on Close.
func (w *Writer) AddRef(rec RefRecord) {
	w.refs = append(w.refs, rec)
}

// AddLog adds a log record to be written.
func (w *Writer) AddLog(rec LogRecord) {
	w.logs = append(w.logs, rec)
}

// Close writes the reftable file (header, ref blocks, log blocks, footer)
// and flushes. After Close, the Writer must not be reused.
func (w *Writer) Close() error {
	if w.blockSize > 0xffffff {
		return fmt.Errorf("reftable: block size %d exceeds 24-bit limit", w.blockSize)
	}
	if w.opts.BlockSize > 0 && w.opts.BlockSize < 1024 {
		return fmt.Errorf("reftable: block size %d is too small (minimum 1024)", w.opts.BlockSize)
	}

	// Sort refs by name.
	sort.Slice(w.refs, func(i, j int) bool {
		return w.refs[i].RefName < w.refs[j].RefName
	})
	// Sort logs by key (refname, then reverse update_index).
	sort.Slice(w.logs, func(i, j int) bool {
		ki := logKey(w.logs[i].RefName, w.logs[i].UpdateIndex)
		kj := logKey(w.logs[j].RefName, w.logs[j].UpdateIndex)
		return ki < kj
	})

	// Write file header.
	if err := w.writeHeader(); err != nil {
		return err
	}

	// Write ref blocks.
	var indexRecs []indexRecord
	if len(w.refs) > 0 {
		if err := w.writeRefBlocks(&indexRecs); err != nil {
			return err
		}
	} else {
		if err := w.flushRefBlock(nil, nil, true); err != nil {
			return err
		}
	}

	// Write index blocks if we have more than 1 ref block.
	var refIndexPos uint64
	if len(indexRecs) > 1 {
		var err error
		refIndexPos, err = w.writeIndexBlocks(indexRecs)
		if err != nil {
			return err
		}
	}

	// Write log blocks.
	logPos := uint64(0)
	if len(w.logs) > 0 {
		logPos = uint64(w.totalWritten)
		if err := w.writeLogBlocks(); err != nil {
			return err
		}
	}

	// Write footer.
	return w.writeFooter(logPos, refIndexPos)
}

func (w *Writer) writeHeader() error {
	header := make([]byte, headerSizeV1)
	copy(header[0:4], magic[:])
	header[4] = versionV1

	// Block size as uint24.
	bs := uint32(w.blockSize)
	header[5] = byte(bs >> 16)
	header[6] = byte(bs >> 8)
	header[7] = byte(bs)

	// Min/max update index.
	binary.BigEndian.PutUint64(header[8:16], w.opts.MinUpdateIndex)
	binary.BigEndian.PutUint64(header[16:24], w.opts.MaxUpdateIndex)

	n, err := w.w.Write(header)
	w.totalWritten += int64(n)
	w.headerWritten = true
	return err
}

func (w *Writer) writeRefBlocks(indexRecs *[]indexRecord) error {
	var buf bytes.Buffer
	prevName := ""
	recordCount := 0
	var restarts []uint32
	isFirstBlock := (w.totalWritten == int64(headerSizeV1))

	flush := func() error {
		if buf.Len() == 0 {
			return nil
		}
		startOffset := uint64(w.totalWritten)
		if isFirstBlock {
			startOffset = 0
		}
		err := w.flushRefBlock(buf.Bytes(), restarts, isFirstBlock)
		isFirstBlock = false
		if err != nil {
			return err
		}
		// Record the last key of this block.
		lastKey := prevName
		*indexRecs = append(*indexRecs, indexRecord{
			LastKey:       lastKey,
			BlockPosition: startOffset,
		})
		return nil
	}

	for i := range w.refs {
		rec := &w.refs[i]
		encoded := encodeRefRecord(rec, prevName, w.hashSize, w.opts.MinUpdateIndex)

		// Compute the total block size including all overhead.
		fileHeaderOverhead := 0
		if isFirstBlock {
			fileHeaderOverhead = headerSizeV1
		}
		restartTableSize := (len(restarts) + 1) * 3 // +1 for potential new restart
		totalSize := fileHeaderOverhead + blockHeaderSize + buf.Len() + len(encoded) + restartTableSize + 2
		if buf.Len() > 0 && totalSize > w.blockSize {
			if err := flush(); err != nil {
				return err
			}
			buf.Reset()
			restarts = nil
			prevName = ""
			recordCount = 0
			encoded = encodeRefRecord(rec, "", w.hashSize, w.opts.MinUpdateIndex)
		}

		if recordCount%w.restartFreq == 0 {
			// Restart offset is relative to the start of the block in the file.
			// For the first block, the block starts at file offset 0 (includes file header).
			// For subsequent blocks, the block starts at the block boundary.
			restartBase := 0
			if isFirstBlock {
				restartBase = headerSizeV1
			}
			restarts = append(restarts, uint32(restartBase+blockHeaderSize+buf.Len()))
			encoded = encodeRefRecord(rec, "", w.hashSize, w.opts.MinUpdateIndex)
		}

		buf.Write(encoded)
		prevName = rec.RefName
		recordCount++
	}

	return flush()
}

func (w *Writer) flushRefBlock(data []byte, restarts []uint32, isFirstBlock bool) error {
	// Build the full block: header + data + restart table + restart count.
	restartTable := make([]byte, len(restarts)*3+2)
	for i, r := range restarts {
		restartTable[i*3] = byte(r >> 16)
		restartTable[i*3+1] = byte(r >> 8)
		restartTable[i*3+2] = byte(r)
	}
	binary.BigEndian.PutUint16(restartTable[len(restarts)*3:], uint16(len(restarts)))

	// blockLen as stored in the header is the total meaningful size from
	// the start of the raw block data (including file header for the first block).
	fileHeaderSize := 0
	if isFirstBlock {
		fileHeaderSize = headerSizeV1
	}
	contentLen := blockHeaderSize + len(data) + len(restartTable)
	blockLen := fileHeaderSize + contentLen

	// Pad to blockSize alignment.
	padding := 0
	if w.blockSize > 0 && blockLen < w.blockSize {
		padding = w.blockSize - blockLen
	}

	header := [blockHeaderSize]byte{
		blockTypeRef,
		byte(blockLen >> 16),
		byte(blockLen >> 8),
		byte(blockLen),
	}

	totalBlock := make([]byte, 0, contentLen+padding)
	totalBlock = append(totalBlock, header[:]...)
	totalBlock = append(totalBlock, data...)
	totalBlock = append(totalBlock, restartTable...)
	if padding > 0 {
		totalBlock = append(totalBlock, make([]byte, padding)...)
	}

	n, err := w.w.Write(totalBlock)
	w.totalWritten += int64(n)
	return err
}

func encodeRefRecord(rec *RefRecord, prevName string, hashSize int, minUpdateIndex uint64) []byte {
	var buf [10]byte
	var out []byte

	// Compute prefix length.
	prefixLen := commonPrefix(prevName, rec.RefName)
	suffix := rec.RefName[prefixLen:]

	// prefix_length varint.
	n := gbinary.PutVarInt(buf[:], uint64(prefixLen))
	out = append(out, buf[:n]...)

	// (suffix_length << 3) | value_type.
	suffixType := (uint64(len(suffix)) << refValueTypeBits) | uint64(rec.ValueType)
	n = gbinary.PutVarInt(buf[:], suffixType)
	out = append(out, buf[:n]...)

	// suffix.
	out = append(out, suffix...)

	// update_index delta.
	delta := rec.UpdateIndex - minUpdateIndex
	n = gbinary.PutVarInt(buf[:], delta)
	out = append(out, buf[:n]...)

	// Value data.
	switch rec.ValueType {
	case refValueDeletion:
		// No value.
	case refValueVal1:
		out = append(out, rec.Value[:hashSize]...)
	case refValueVal2:
		out = append(out, rec.Value[:hashSize]...)
		out = append(out, rec.TargetValue[:hashSize]...)
	case refValueSymref:
		n = gbinary.PutVarInt(buf[:], uint64(len(rec.Target)))
		out = append(out, buf[:n]...)
		out = append(out, rec.Target...)
	}

	return out
}

func (w *Writer) writeLogBlocks() error {
	logs := w.logs
	for len(logs) > 0 {
		k := 1
		var lastCompressed []byte
		var lastInflatedLen int

		for k <= len(logs) {
			compressed, inflatedLen, err := w.tryCompressLogs(logs[:k])
			if err != nil {
				return err
			}

			if w.blockSize > 0 && blockHeaderSize+len(compressed) > w.blockSize {
				if k == 1 {
					lastCompressed = compressed
					lastInflatedLen = inflatedLen
					break
				}
				k--
				break
			}

			lastCompressed = compressed
			lastInflatedLen = inflatedLen

			if k == len(logs) {
				break
			}
			k++
		}

		if err := w.writeCompressedLogBlock(lastCompressed, lastInflatedLen); err != nil {
			return err
		}

		logs = logs[k:]
	}
	return nil
}

func (w *Writer) tryCompressLogs(logs []LogRecord) ([]byte, int, error) {
	var recordBuf bytes.Buffer
	prevKey := ""
	var restarts []uint32

	for i := range logs {
		rec := &logs[i]
		if i%w.restartFreq == 0 {
			restarts = append(restarts, uint32(blockHeaderSize+recordBuf.Len()))
			prevKey = ""
		}
		encoded := encodeLogRecord(rec, prevKey, w.hashSize)
		recordBuf.Write(encoded)
		prevKey = logKey(rec.RefName, rec.UpdateIndex)
	}

	restartTable := make([]byte, len(restarts)*3+2)
	for i, r := range restarts {
		restartTable[i*3] = byte(r >> 16)
		restartTable[i*3+1] = byte(r >> 8)
		restartTable[i*3+2] = byte(r)
	}
	binary.BigEndian.PutUint16(restartTable[len(restarts)*3:], uint16(len(restarts)))

	inflated := append(recordBuf.Bytes(), restartTable...)
	inflatedLen := len(inflated)

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(inflated); err != nil {
		return nil, 0, err
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}

	return compressed.Bytes(), inflatedLen, nil
}

func (w *Writer) writeCompressedLogBlock(compressed []byte, inflatedLen int) error {
	inflatedBlockLen := blockHeaderSize + inflatedLen
	if inflatedBlockLen > 0xffffff {
		return fmt.Errorf("reftable: log block inflated size %d exceeds 24-bit limit", inflatedBlockLen)
	}
	header := [blockHeaderSize]byte{
		blockTypeLog,
		byte(inflatedBlockLen >> 16),
		byte(inflatedBlockLen >> 8),
		byte(inflatedBlockLen),
	}

	n, err := w.w.Write(header[:])
	w.totalWritten += int64(n)
	if err != nil {
		return err
	}

	n, err = w.w.Write(compressed)
	w.totalWritten += int64(n)
	if err != nil {
		return err
	}

	if w.blockSize > 0 {
		written := blockHeaderSize + len(compressed)
		padding := w.blockSize - (written % w.blockSize)
		if padding < w.blockSize {
			padBytes := make([]byte, padding)
			n, err = w.w.Write(padBytes)
			w.totalWritten += int64(n)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func encodeLogRecord(rec *LogRecord, prevKey string, hashSize int) []byte {
	var buf [10]byte
	var out []byte

	key := logKey(rec.RefName, rec.UpdateIndex)

	prefixLen := commonPrefix(prevKey, key)
	suffix := key[prefixLen:]

	// prefix_length varint.
	n := gbinary.PutVarInt(buf[:], uint64(prefixLen))
	out = append(out, buf[:n]...)

	// (suffix_length << 3) | log_type.
	suffixType := (uint64(len(suffix)) << 3) | uint64(rec.LogType)
	n = gbinary.PutVarInt(buf[:], suffixType)
	out = append(out, buf[:n]...)

	// suffix.
	out = append(out, suffix...)

	if rec.LogType == logValueDeletion {
		return out
	}

	// old_hash, new_hash.
	if len(rec.OldHash) >= hashSize {
		out = append(out, rec.OldHash[:hashSize]...)
	} else {
		out = append(out, make([]byte, hashSize)...)
	}
	if len(rec.NewHash) >= hashSize {
		out = append(out, rec.NewHash[:hashSize]...)
	} else {
		out = append(out, make([]byte, hashSize)...)
	}

	// name.
	n = gbinary.PutVarInt(buf[:], uint64(len(rec.Name)))
	out = append(out, buf[:n]...)
	out = append(out, rec.Name...)

	// email.
	n = gbinary.PutVarInt(buf[:], uint64(len(rec.Email)))
	out = append(out, buf[:n]...)
	out = append(out, rec.Email...)

	// time_seconds.
	n = gbinary.PutVarInt(buf[:], uint64(rec.Time.Unix()))
	out = append(out, buf[:n]...)

	// tz_offset (sint16).
	out = append(out, byte(rec.TZOffset>>8), byte(rec.TZOffset))

	// message.
	n = gbinary.PutVarInt(buf[:], uint64(len(rec.Message)))
	out = append(out, buf[:n]...)
	out = append(out, rec.Message...)

	return out
}

func (w *Writer) writeFooter(logPos, refIndexPos uint64) error {
	footer := make([]byte, footerSizeV1)

	copy(footer[0:4], magic[:])
	footer[4] = versionV1

	// Block size as uint24.
	bs := uint32(w.blockSize)
	footer[5] = byte(bs >> 16)
	footer[6] = byte(bs >> 8)
	footer[7] = byte(bs)

	// Min/max update index.
	binary.BigEndian.PutUint64(footer[8:16], w.opts.MinUpdateIndex)
	binary.BigEndian.PutUint64(footer[16:24], w.opts.MaxUpdateIndex)

	pos := 24
	// ref_index_position.
	binary.BigEndian.PutUint64(footer[pos:], refIndexPos)
	pos += 8

	// obj_position << 5 | obj_id_len = 0.
	binary.BigEndian.PutUint64(footer[pos:], 0)
	pos += 8

	// obj_index_position = 0.
	binary.BigEndian.PutUint64(footer[pos:], 0)
	pos += 8

	// log_position.
	binary.BigEndian.PutUint64(footer[pos:], logPos)
	pos += 8

	// log_index_position = 0.
	binary.BigEndian.PutUint64(footer[pos:], 0)
	pos += 8

	// CRC-32 of everything except the last 4 bytes.
	crc := crc32.ChecksumIEEE(footer[:pos])
	binary.BigEndian.PutUint32(footer[pos:], crc)

	n, err := w.w.Write(footer)
	w.totalWritten += int64(n)
	return err
}

func (w *Writer) writeIndexBlocks(recs []indexRecord) (uint64, error) {
	if len(recs) == 0 {
		return 0, nil
	}
	if len(recs) == 1 {
		return recs[0].BlockPosition, nil
	}

	var parentRecs []indexRecord
	var buf bytes.Buffer
	var restarts []uint32
	prevKey := ""
	lastKeyInBlock := ""
	recordCount := 0

	flushIndex := func() error {
		if buf.Len() == 0 {
			return nil
		}
		startOffset := uint64(w.totalWritten)
		err := w.flushIndexBlock(buf.Bytes(), restarts)
		if err != nil {
			return err
		}

		parentRecs = append(parentRecs, indexRecord{
			LastKey:       lastKeyInBlock,
			BlockPosition: startOffset,
		})
		return nil
	}

	for i := range recs {
		rec := &recs[i]
		encoded := encodeIndexRecord(*rec, prevKey)

		restartTableSize := (len(restarts) + 1) * 3
		totalSize := blockHeaderSize + buf.Len() + len(encoded) + restartTableSize + 2
		if buf.Len() > 0 && totalSize > w.blockSize {
			if err := flushIndex(); err != nil {
				return 0, err
			}
			buf.Reset()
			restarts = nil
			lastKeyInBlock = ""
			recordCount = 0
		}

		if recordCount%w.restartFreq == 0 {
			restarts = append(restarts, uint32(blockHeaderSize+buf.Len()))
			encoded = encodeIndexRecord(*rec, "")
		}

		buf.Write(encoded)
		prevKey = rec.LastKey
		lastKeyInBlock = rec.LastKey
		recordCount++
	}

	if err := flushIndex(); err != nil {
		return 0, err
	}

	return w.writeIndexBlocks(parentRecs)
}

func (w *Writer) flushIndexBlock(data []byte, restarts []uint32) error {
	restartTable := make([]byte, len(restarts)*3+2)
	for i, r := range restarts {
		restartTable[i*3] = byte(r >> 16)
		restartTable[i*3+1] = byte(r >> 8)
		restartTable[i*3+2] = byte(r)
	}
	binary.BigEndian.PutUint16(restartTable[len(restarts)*3:], uint16(len(restarts)))

	contentLen := blockHeaderSize + len(data) + len(restartTable)
	blockLen := contentLen

	padding := 0
	if w.blockSize > 0 && blockLen < w.blockSize {
		padding = w.blockSize - blockLen
	}

	header := [blockHeaderSize]byte{
		blockTypeIndex,
		byte(blockLen >> 16),
		byte(blockLen >> 8),
		byte(blockLen),
	}

	totalBlock := make([]byte, 0, contentLen+padding)
	totalBlock = append(totalBlock, header[:]...)
	totalBlock = append(totalBlock, data...)
	totalBlock = append(totalBlock, restartTable...)
	if padding > 0 {
		totalBlock = append(totalBlock, make([]byte, padding)...)
	}

	n, err := w.w.Write(totalBlock)
	w.totalWritten += int64(n)
	return err
}

func encodeIndexRecord(rec indexRecord, prevKey string) []byte {
	var buf [10]byte

	prefixLen := commonPrefix(prevKey, rec.LastKey)
	suffix := rec.LastKey[prefixLen:]

	out := make([]byte, 0, 30+len(suffix))

	n := gbinary.PutVarInt(buf[:], uint64(prefixLen))
	out = append(out, buf[:n]...)

	suffixType := uint64(len(suffix)) << 3
	n = gbinary.PutVarInt(buf[:], suffixType)
	out = append(out, buf[:n]...)

	out = append(out, suffix...)

	n = gbinary.PutVarInt(buf[:], rec.BlockPosition)
	out = append(out, buf[:n]...)

	return out
}

// commonPrefix returns the length of the common prefix between a and b.
func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
