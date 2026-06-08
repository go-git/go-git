package reftable

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriterRoundTrip(t *testing.T) {
	t.Parallel()

	hashBytes := func(s string) []byte {
		b, err := hex.DecodeString(s)
		require.NoError(t, err)
		return b
	}

	refs := []RefRecord{
		{RefName: "HEAD", ValueType: refValueSymref, Target: "refs/heads/main", UpdateIndex: 1},
		{RefName: "refs/heads/feature", ValueType: refValueVal1, Value: hashBytes("057b41687e44e63ac774d827e64f95b8a383912a"), UpdateIndex: 2},
		{RefName: "refs/heads/main", ValueType: refValueVal1, Value: hashBytes("057b41687e44e63ac774d827e64f95b8a383912a"), UpdateIndex: 1},
		{RefName: "refs/tags/v1.0", ValueType: refValueVal2, Value: hashBytes("9e9e03cb2ab761b9a888da292e5066d0939b1221"), TargetValue: hashBytes("057b41687e44e63ac774d827e64f95b8a383912a"), UpdateIndex: 3},
	}

	var buf bytes.Buffer
	w := NewWriter(&buf, WriterOptions{
		MinUpdateIndex: 1,
		MaxUpdateIndex: 3,
	})
	for _, r := range refs {
		w.AddRef(r)
	}
	require.NoError(t, w.Close())

	// Read back.
	data := buf.Bytes()
	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)

	// Verify footer.
	assert.Equal(t, versionV1, tbl.footer.version)
	assert.Equal(t, uint64(1), tbl.footer.minUpdateIndex)
	assert.Equal(t, uint64(3), tbl.footer.maxUpdateIndex)

	// Verify refs can be looked up.
	head, err := tbl.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, head)
	assert.Equal(t, uint8(refValueSymref), head.ValueType)
	assert.Equal(t, "refs/heads/main", head.Target)

	main, err := tbl.Ref("refs/heads/main")
	require.NoError(t, err)
	require.NotNil(t, main)
	assert.Equal(t, "057b41687e44e63ac774d827e64f95b8a383912a", hex.EncodeToString(main.Value))

	tag, err := tbl.Ref("refs/tags/v1.0")
	require.NoError(t, err)
	require.NotNil(t, tag)
	assert.Equal(t, uint8(refValueVal2), tag.ValueType)
	assert.Equal(t, "9e9e03cb2ab761b9a888da292e5066d0939b1221", hex.EncodeToString(tag.Value))
	assert.Equal(t, "057b41687e44e63ac774d827e64f95b8a383912a", hex.EncodeToString(tag.TargetValue))

	// Verify iteration.
	var names []string
	err = tbl.IterRefs(func(rec RefRecord) bool {
		names = append(names, rec.RefName)
		return true
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"HEAD", "refs/heads/feature", "refs/heads/main", "refs/tags/v1.0"}, names)
}

func TestWriterDeletion(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, WriterOptions{
		MinUpdateIndex: 1,
		MaxUpdateIndex: 1,
	})
	w.AddRef(RefRecord{
		RefName:     "refs/heads/gone",
		ValueType:   refValueDeletion,
		UpdateIndex: 1,
	})
	require.NoError(t, w.Close())

	data := buf.Bytes()
	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)

	rec, err := tbl.Ref("refs/heads/gone")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, uint8(refValueDeletion), rec.ValueType)
}

func TestWriterWithLogs(t *testing.T) {
	t.Parallel()

	hashBytes := func(s string) []byte {
		b, err := hex.DecodeString(s)
		require.NoError(t, err)
		return b
	}

	var buf bytes.Buffer
	w := NewWriter(&buf, WriterOptions{
		MinUpdateIndex: 1,
		MaxUpdateIndex: 1,
	})
	w.AddRef(RefRecord{
		RefName:     "refs/heads/main",
		ValueType:   refValueVal1,
		Value:       hashBytes("057b41687e44e63ac774d827e64f95b8a383912a"),
		UpdateIndex: 1,
	})
	w.AddLog(LogRecord{
		RefName:     "refs/heads/main",
		UpdateIndex: 1,
		LogType:     logValueUpdate,
		OldHash:     make([]byte, 20),
		NewHash:     hashBytes("057b41687e44e63ac774d827e64f95b8a383912a"),
		Name:        "Test User",
		Email:       "test@example.com",
		Message:     "initial commit",
	})
	require.NoError(t, w.Close())

	data := buf.Bytes()
	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)

	// Verify log.
	var logs []LogRecord
	err = tbl.IterLogs(func(rec LogRecord) bool {
		logs = append(logs, rec)
		return true
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "refs/heads/main", logs[0].RefName)
	assert.Equal(t, "Test User", logs[0].Name)
	assert.Equal(t, "initial commit", logs[0].Message)
}

func TestWriterMultiBlockRoundTrip(t *testing.T) {
	t.Parallel()

	// Create enough refs to span multiple blocks with a small block size.
	const numRefs = 200
	const blockSize = 1024

	refs := make([]RefRecord, 0, numRefs)
	for i := range numRefs {
		h := sha1.Sum(fmt.Appendf(nil, "ref-%04d", i))
		refs = append(refs, RefRecord{
			RefName:     fmt.Sprintf("refs/heads/branch-%04d", i),
			ValueType:   refValueVal1,
			Value:       h[:],
			UpdateIndex: uint64(i + 1),
		})
	}

	var buf bytes.Buffer
	w := NewWriter(&buf, WriterOptions{
		BlockSize:      blockSize,
		MinUpdateIndex: 1,
		MaxUpdateIndex: uint64(numRefs),
	})
	for _, r := range refs {
		w.AddRef(r)
	}
	require.NoError(t, w.Close())

	data := buf.Bytes()
	t.Logf("table size: %d bytes (expect multiple %d-byte blocks)", len(data), blockSize)

	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)
	assert.True(t, tbl.footer.refIndexPos > 0, "expected non-zero refIndexPos for multi-block table")

	// Verify every ref can be looked up individually.
	for _, ref := range refs {
		rec, err := tbl.Ref(ref.RefName)
		require.NoError(t, err, "lookup failed for %s", ref.RefName)
		require.NotNil(t, rec, "ref %s not found", ref.RefName)
		assert.Equal(t, ref.RefName, rec.RefName)
		assert.Equal(t, hex.EncodeToString(ref.Value), hex.EncodeToString(rec.Value))
	}

	// Verify iteration returns all refs.
	var names []string
	err = tbl.IterRefs(func(rec RefRecord) bool {
		names = append(names, rec.RefName)
		return true
	})
	require.NoError(t, err)
	assert.Len(t, names, numRefs)
}

func TestTableIterLogsMultiBlock(t *testing.T) {
	t.Parallel()

	// 1. Create table A with log A
	var bufA bytes.Buffer
	wA := NewWriter(&bufA, WriterOptions{
		MinUpdateIndex: 1,
		MaxUpdateIndex: 1,
	})
	wA.AddLog(LogRecord{
		RefName:     "refs/heads/main",
		UpdateIndex: 1,
		LogType:     logValueUpdate,
		Name:        "User A",
		Message:     "commit A",
	})
	require.NoError(t, wA.Close())

	// 2. Create table B with log B
	var bufB bytes.Buffer
	wB := NewWriter(&bufB, WriterOptions{
		MinUpdateIndex: 2,
		MaxUpdateIndex: 2,
	})
	wB.AddLog(LogRecord{
		RefName:     "refs/heads/main",
		UpdateIndex: 2,
		LogType:     logValueUpdate,
		Name:        "User B",
		Message:     "commit B",
	})
	require.NoError(t, wB.Close())

	// Open table A to find log Pos and size
	tblA, err := OpenTable(newBytesReaderAt(bufA.Bytes()), int64(bufA.Len()))
	require.NoError(t, err)
	logStartA := tblA.footer.logPos
	logEndA := uint64(bufA.Len() - footerSizeV1)
	logBytesA := bufA.Bytes()[logStartA:logEndA]

	// Open table B to find log Pos and size
	tblB, err := OpenTable(newBytesReaderAt(bufB.Bytes()), int64(bufB.Len()))
	require.NoError(t, err)
	logStartB := tblB.footer.logPos
	logEndB := uint64(bufB.Len() - footerSizeV1)
	logBytesB := bufB.Bytes()[logStartB:logEndB]

	// 3. Assemble combined bytes:
	// - Everything up to logStartA from table A
	// - logBytesA
	// - logBytesB
	// - New footer with updated logPos and CRC
	combined := append([]byte(nil), bufA.Bytes()[:logStartA]...)
	combined = append(combined, logBytesA...)
	combined = append(combined, logBytesB...)

	// Update footer.
	newFooterPos := len(combined)
	footerData := make([]byte, footerSizeV1)

	copy(footerData, magic[:])
	footerData[4] = versionV1

	// block size (uint24)
	footerData[5] = byte(tblA.footer.blockSize >> 16)
	footerData[6] = byte(tblA.footer.blockSize >> 8)
	footerData[7] = byte(tblA.footer.blockSize)

	binary.BigEndian.PutUint64(footerData[8:], tblA.footer.minUpdateIndex)
	binary.BigEndian.PutUint64(footerData[16:], tblB.footer.maxUpdateIndex) // update max update index
	binary.BigEndian.PutUint64(footerData[24:], 0)                          // ref index pos

	objData := uint64(0) // no obj index
	binary.BigEndian.PutUint64(footerData[32:], objData)
	binary.BigEndian.PutUint64(footerData[40:], 0)                 // obj index pos
	binary.BigEndian.PutUint64(footerData[48:], uint64(logStartA)) // logPos starts at logStartA
	binary.BigEndian.PutUint64(footerData[56:], 0)                 // log index pos

	// CRC
	crc := crc32.ChecksumIEEE(footerData[:footerSizeV1-4])
	binary.BigEndian.PutUint32(footerData[footerSizeV1-4:], crc)

	combined = append(combined, footerData...)

	// Read back
	tblCombined, err := OpenTable(newBytesReaderAt(combined), int64(newFooterPos+footerSizeV1))
	require.NoError(t, err)

	// Verify we can read both logs!
	var logs []LogRecord
	err = tblCombined.IterLogs(func(rec LogRecord) bool {
		logs = append(logs, rec)
		return true
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	assert.Equal(t, "User A", logs[0].Name)
	assert.Equal(t, "User B", logs[1].Name)
}

func TestWriterSHA256(t *testing.T) {
	t.Parallel()

	hashBytes := func(s string) []byte {
		b, err := hex.DecodeString(s)
		require.NoError(t, err)
		return b
	}

	var buf bytes.Buffer
	w := NewWriter(&buf, WriterOptions{
		MinUpdateIndex: 1,
		MaxUpdateIndex: 2,
		HashSize:       32, // SHA-256
	})

	w.AddRef(RefRecord{
		RefName:     "refs/heads/main",
		ValueType:   refValueVal1,
		Value:       hashBytes("057b41687e44e63ac774d827e64f95b8a383912a057b41687e44e63ac774d827"),
		UpdateIndex: 1,
	})
	w.AddLog(LogRecord{
		RefName:     "refs/heads/main",
		UpdateIndex: 2,
		LogType:     logValueUpdate,
		OldHash:     make([]byte, 32),
		NewHash:     hashBytes("057b41687e44e63ac774d827e64f95b8a383912a057b41687e44e63ac774d827"),
		Name:        "SHA256 User",
		Message:     "sha256 commit",
	})

	require.NoError(t, w.Close())

	data := buf.Bytes()
	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)

	assert.Equal(t, versionV2, tbl.footer.version)
	assert.Equal(t, 32, tbl.hashSize)

	rec, err := tbl.Ref("refs/heads/main")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, uint8(refValueVal1), rec.ValueType)
	assert.Equal(t, 32, len(rec.Value))

	var logs []LogRecord
	err = tbl.IterLogs(func(log LogRecord) bool {
		logs = append(logs, log)
		return true
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "SHA256 User", logs[0].Name)
	assert.Equal(t, 32, len(logs[0].NewHash))
}
