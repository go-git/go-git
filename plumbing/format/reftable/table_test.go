package reftable

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestTable(t *testing.T) *Table {
	t.Helper()
	data, err := os.ReadFile("testdata/repo1/reftable/0x000000000001-0x000000000006-6b4a0580.ref")
	require.NoError(t, err)

	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)
	return tbl
}

func TestTableFooter(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	assert.Equal(t, versionV1, tbl.footer.version)
	assert.Equal(t, 20, tbl.hashSize)
	assert.Equal(t, uint64(1), tbl.footer.minUpdateIndex)
	assert.Equal(t, uint64(6), tbl.footer.maxUpdateIndex)
}

func TestTableRef(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	tests := []struct {
		name     string
		wantHash string
		wantType uint8
	}{
		{"HEAD", "", refValueSymref},
		{"refs/heads/main", "057b41687e44e63ac774d827e64f95b8a383912a", refValueVal1},
		{"refs/heads/feature", "057b41687e44e63ac774d827e64f95b8a383912a", refValueVal1},
		{"refs/tags/v1.0", "9e9e03cb2ab761b9a888da292e5066d0939b1221", refValueVal1},
		{"refs/tags/v2.0", "39d5a02d1e245cb909d528afb9bb2ce8775b3568", refValueVal2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec, err := tbl.Ref(tt.name)
			require.NoError(t, err)
			require.NotNil(t, rec, "ref %s not found", tt.name)

			assert.Equal(t, tt.name, rec.RefName)
			assert.Equal(t, tt.wantType, rec.ValueType)

			if tt.wantHash != "" {
				got := hex.EncodeToString(rec.Value)
				assert.Equal(t, tt.wantHash, got)
			}
		})
	}
}

func TestTableRefHEADSymbolic(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	rec, err := tbl.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, uint8(refValueSymref), rec.ValueType)
	assert.Equal(t, "refs/heads/main", rec.Target)
}

func TestTableRefAnnotatedTag(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	rec, err := tbl.Ref("refs/tags/v2.0")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, uint8(refValueVal2), rec.ValueType)
	assert.Equal(t, "39d5a02d1e245cb909d528afb9bb2ce8775b3568", hex.EncodeToString(rec.Value))
	assert.Equal(t, "057b41687e44e63ac774d827e64f95b8a383912a", hex.EncodeToString(rec.TargetValue))
}

func TestTableRefNotFound(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	rec, err := tbl.Ref("refs/heads/nonexistent")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestTableIterRefs(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	var names []string
	err := tbl.IterRefs(func(rec RefRecord) bool {
		names = append(names, rec.RefName)
		return true
	})
	require.NoError(t, err)

	assert.Contains(t, names, "HEAD")
	assert.Contains(t, names, "refs/heads/main")
	assert.Contains(t, names, "refs/heads/feature")
	assert.Contains(t, names, "refs/tags/v1.0")
	assert.Contains(t, names, "refs/tags/v2.0")
}

func TestTableIterLogs(t *testing.T) {
	t.Parallel()
	tbl := openTestTable(t)

	var entries []LogRecord
	err := tbl.IterLogs(func(rec LogRecord) bool {
		entries = append(entries, rec)
		return true
	})
	require.NoError(t, err)

	// Should have reflog entries for HEAD, refs/heads/main, etc.
	assert.Greater(t, len(entries), 0, "expected at least one log entry")

	// Check that log entries have valid data.
	for _, e := range entries {
		assert.NotEmpty(t, e.RefName, "log entry should have a ref name")
		assert.NotEmpty(t, e.Name, "log entry should have committer name")
		assert.NotEmpty(t, e.Email, "log entry should have committer email")
	}
}
