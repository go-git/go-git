package reftable

import (
	"bytes"
	"encoding/hex"
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
		b, _ := hex.DecodeString(s)
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
