package reftable

import (
	"fmt"
	"time"

	gbinary "github.com/go-git/go-git/v6/utils/binary"
)

// Record value types for ref records.
const (
	refValueDeletion = 0 // tombstone
	refValueVal1     = 1 // one object ID (direct ref)
	refValueVal2     = 2 // two object IDs (ref + peeled target)
	refValueSymref   = 3 // symbolic reference
	refValueTypeMax  = 3
	refValueTypeMask = 0x7
	refValueTypeBits = 3
)

// Record value types for log records.
const (
	logValueDeletion = 0 // tombstone
	logValueUpdate   = 1 // standard reflog entry
)

// RefRecord represents a single reference record from a reftable.
type RefRecord struct {
	RefName     string
	UpdateIndex uint64
	ValueType   uint8
	Value       []byte // object ID for value_type 1 or 2
	TargetValue []byte // peeled object ID for value_type 2
	Target      string // symbolic ref target for value_type 3
}

// LogRecord represents a single reflog record from a reftable.
type LogRecord struct {
	RefName     string
	UpdateIndex uint64
	LogType     uint8
	OldHash     []byte
	NewHash     []byte
	Name        string
	Email       string
	Time        time.Time
	TZOffset    int16
	Message     string
}

// decodeRefRecord decodes a ref record from buf at the given position.
// prefixName is the name prefix carried over from the previous record.
// hashSize is the size of object IDs in bytes (20 for SHA-1, 32 for SHA-256).
// minUpdateIndex is the base for the update_index delta.
// Returns the decoded record and the number of bytes consumed.
func decodeRefRecord(buf []byte, prefixName string, hashSize int, minUpdateIndex uint64) (RefRecord, int, error) {
	if len(buf) == 0 {
		return RefRecord{}, 0, fmt.Errorf("%w: empty ref record", ErrCorruptBlock)
	}

	pos := 0

	// Decode prefix_length.
	prefixLen, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return RefRecord{}, 0, fmt.Errorf("%w: truncated prefix_length", ErrCorruptBlock)
	}
	pos += n

	// Decode (suffix_length << 3) | value_type.
	suffixTypeVal, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return RefRecord{}, 0, fmt.Errorf("%w: truncated suffix_type", ErrCorruptBlock)
	}
	pos += n

	valueType := uint8(suffixTypeVal & refValueTypeMask)
	suffixLen := int(suffixTypeVal >> refValueTypeBits)

	if valueType > refValueTypeMax {
		return RefRecord{}, 0, fmt.Errorf("%w: unknown ref value type %d", ErrCorruptBlock, valueType)
	}

	// Reconstruct full name.
	if int(prefixLen) > len(prefixName) {
		return RefRecord{}, 0, fmt.Errorf("%w: prefix_length %d exceeds previous name length %d", ErrCorruptBlock, prefixLen, len(prefixName))
	}
	if pos+suffixLen > len(buf) {
		return RefRecord{}, 0, fmt.Errorf("%w: suffix extends beyond block", ErrCorruptBlock)
	}

	name := prefixName[:prefixLen] + string(buf[pos:pos+suffixLen])
	pos += suffixLen

	// Decode update_index delta.
	updateDelta, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return RefRecord{}, 0, fmt.Errorf("%w: truncated update_index", ErrCorruptBlock)
	}
	pos += n

	rec := RefRecord{
		RefName:     name,
		UpdateIndex: minUpdateIndex + updateDelta,
		ValueType:   valueType,
	}

	switch valueType {
	case refValueDeletion:
		// No value.
	case refValueVal1:
		if pos+hashSize > len(buf) {
			return RefRecord{}, 0, fmt.Errorf("%w: truncated object ID", ErrCorruptBlock)
		}
		rec.Value = make([]byte, hashSize)
		copy(rec.Value, buf[pos:pos+hashSize])
		pos += hashSize
	case refValueVal2:
		if pos+2*hashSize > len(buf) {
			return RefRecord{}, 0, fmt.Errorf("%w: truncated object IDs", ErrCorruptBlock)
		}
		rec.Value = make([]byte, hashSize)
		copy(rec.Value, buf[pos:pos+hashSize])
		pos += hashSize
		rec.TargetValue = make([]byte, hashSize)
		copy(rec.TargetValue, buf[pos:pos+hashSize])
		pos += hashSize
	case refValueSymref:
		targetLen, n := gbinary.GetVarInt(buf[pos:])
		if n == 0 {
			return RefRecord{}, 0, fmt.Errorf("%w: truncated symref target length", ErrCorruptBlock)
		}
		pos += n
		if pos+int(targetLen) > len(buf) {
			return RefRecord{}, 0, fmt.Errorf("%w: symref target extends beyond block", ErrCorruptBlock)
		}
		rec.Target = string(buf[pos : pos+int(targetLen)])
		pos += int(targetLen)
	}

	return rec, pos, nil
}

// decodeLogRecord decodes a log record from buf at the given position.
// prefixKey is the key prefix carried over from the previous record.
// hashSize is the size of object IDs in bytes.
// Returns the decoded record and the number of bytes consumed.
func decodeLogRecord(buf []byte, prefixKey string, hashSize int) (LogRecord, int, error) {
	if len(buf) == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: empty log record", ErrCorruptBlock)
	}

	pos := 0

	// Decode prefix_length.
	prefixLen, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated prefix_length", ErrCorruptBlock)
	}
	pos += n

	// Decode (suffix_length << 3) | log_type.
	suffixTypeVal, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated suffix_type", ErrCorruptBlock)
	}
	pos += n

	logType := uint8(suffixTypeVal & 0x7)
	suffixLen := int(suffixTypeVal >> 3)

	// Reconstruct full key: refname \0 reverse_int64(update_index)
	if int(prefixLen) > len(prefixKey) {
		return LogRecord{}, 0, fmt.Errorf("%w: prefix_length %d exceeds previous key length %d", ErrCorruptBlock, prefixLen, len(prefixKey))
	}
	if pos+suffixLen > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: suffix extends beyond block", ErrCorruptBlock)
	}

	key := prefixKey[:prefixLen] + string(buf[pos:pos+suffixLen])
	pos += suffixLen

	// Parse key into refname and update_index.
	refName, updateIndex, err := parseLogKey(key)
	if err != nil {
		return LogRecord{}, 0, err
	}

	rec := LogRecord{
		RefName:     refName,
		UpdateIndex: updateIndex,
		LogType:     logType,
	}

	if logType == logValueDeletion {
		return rec, pos, nil
	}

	if logType != logValueUpdate {
		return LogRecord{}, 0, fmt.Errorf("%w: unknown log value type %d", ErrCorruptBlock, logType)
	}

	// Decode log_data for type 1.
	// old_id
	if pos+hashSize > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated old_id", ErrCorruptBlock)
	}
	rec.OldHash = make([]byte, hashSize)
	copy(rec.OldHash, buf[pos:pos+hashSize])
	pos += hashSize

	// new_id
	if pos+hashSize > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated new_id", ErrCorruptBlock)
	}
	rec.NewHash = make([]byte, hashSize)
	copy(rec.NewHash, buf[pos:pos+hashSize])
	pos += hashSize

	// name
	nameLen, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated name_length", ErrCorruptBlock)
	}
	pos += n
	if pos+int(nameLen) > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: name extends beyond block", ErrCorruptBlock)
	}
	rec.Name = string(buf[pos : pos+int(nameLen)])
	pos += int(nameLen)

	// email
	emailLen, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated email_length", ErrCorruptBlock)
	}
	pos += n
	if pos+int(emailLen) > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: email extends beyond block", ErrCorruptBlock)
	}
	rec.Email = string(buf[pos : pos+int(emailLen)])
	pos += int(emailLen)

	// time_seconds (varint)
	timeSec, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated time_seconds", ErrCorruptBlock)
	}
	pos += n

	// tz_offset (sint16, big-endian)
	if pos+2 > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated tz_offset", ErrCorruptBlock)
	}
	rec.TZOffset = int16(buf[pos])<<8 | int16(buf[pos+1])
	pos += 2

	// Convert to time.Time.
	tzMinutes := int(rec.TZOffset)
	loc := time.FixedZone("", tzMinutes*60)
	rec.Time = time.Unix(int64(timeSec), 0).In(loc)

	// message
	msgLen, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return LogRecord{}, 0, fmt.Errorf("%w: truncated message_length", ErrCorruptBlock)
	}
	pos += n
	if pos+int(msgLen) > len(buf) {
		return LogRecord{}, 0, fmt.Errorf("%w: message extends beyond block", ErrCorruptBlock)
	}
	rec.Message = string(buf[pos : pos+int(msgLen)])
	pos += int(msgLen)

	return rec, pos, nil
}

// parseLogKey extracts refname and update_index from a log record key.
// The key format is: refname \0 reverse_int64(update_index)
// where reverse_int64(t) = 0xffffffffffffffff - t.
func parseLogKey(key string) (string, uint64, error) {
	// Find the NUL separator.
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			if i+9 != len(key) {
				return "", 0, fmt.Errorf("%w: log key has wrong size after NUL: expected 8 bytes, got %d", ErrCorruptBlock, len(key)-i-1)
			}
			refName := key[:i]
			// Read 8 bytes big-endian as reverse_int64.
			b := []byte(key[i+1:])
			rev := uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
				uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
			updateIndex := ^rev // 0xffffffffffffffff - rev == ^rev
			return refName, updateIndex, nil
		}
	}
	return "", 0, fmt.Errorf("%w: log key missing NUL separator", ErrCorruptBlock)
}

// indexRecord represents a single index record pointing to a block.
type indexRecord struct {
	LastKey       string
	BlockPosition uint64
}

// decodeIndexRecord decodes an index record from buf.
func decodeIndexRecord(buf []byte, prefixKey string) (indexRecord, int, error) {
	if len(buf) == 0 {
		return indexRecord{}, 0, fmt.Errorf("%w: empty index record", ErrCorruptBlock)
	}

	pos := 0

	prefixLen, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return indexRecord{}, 0, fmt.Errorf("%w: truncated prefix_length", ErrCorruptBlock)
	}
	pos += n

	suffixTypeVal, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return indexRecord{}, 0, fmt.Errorf("%w: truncated suffix_type", ErrCorruptBlock)
	}
	pos += n

	// Type is always 0 for index records.
	suffixLen := int(suffixTypeVal >> 3)

	if int(prefixLen) > len(prefixKey) {
		return indexRecord{}, 0, fmt.Errorf("%w: prefix_length exceeds previous key", ErrCorruptBlock)
	}
	if pos+suffixLen > len(buf) {
		return indexRecord{}, 0, fmt.Errorf("%w: suffix extends beyond block", ErrCorruptBlock)
	}

	key := prefixKey[:prefixLen] + string(buf[pos:pos+suffixLen])
	pos += suffixLen

	blockPos, n := gbinary.GetVarInt(buf[pos:])
	if n == 0 {
		return indexRecord{}, 0, fmt.Errorf("%w: truncated block_position", ErrCorruptBlock)
	}
	pos += n

	return indexRecord{LastKey: key, BlockPosition: blockPos}, pos, nil
}
