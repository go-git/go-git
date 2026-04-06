package reftable

import (
	"encoding/hex"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStack(t *testing.T) *Stack {
	t.Helper()
	fs := osfs.New("testdata/repo1/reftable")
	stack, err := OpenStack(fs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stack.Close() })
	return stack
}

func TestStackRef(t *testing.T) {
	t.Parallel()
	stack := openTestStack(t)

	tests := []struct {
		name     string
		wantHash string
		wantNil  bool
	}{
		{"refs/heads/main", "057b41687e44e63ac774d827e64f95b8a383912a", false},
		{"refs/heads/feature", "057b41687e44e63ac774d827e64f95b8a383912a", false},
		{"refs/tags/v1.0", "9e9e03cb2ab761b9a888da292e5066d0939b1221", false},
		{"refs/heads/nonexistent", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec, err := stack.Ref(tt.name)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, rec)
			} else {
				require.NotNil(t, rec)
				assert.Equal(t, tt.wantHash, hex.EncodeToString(rec.Value))
			}
		})
	}
}

func TestStackRefHEAD(t *testing.T) {
	t.Parallel()
	stack := openTestStack(t)

	rec, err := stack.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, uint8(refValueSymref), rec.ValueType)
	assert.Equal(t, "refs/heads/main", rec.Target)
}

func TestStackIterRefs(t *testing.T) {
	t.Parallel()
	stack := openTestStack(t)

	var names []string
	err := stack.IterRefs(func(rec RefRecord) bool {
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

func TestStackLogsFor(t *testing.T) {
	t.Parallel()
	stack := openTestStack(t)

	logs, err := stack.LogsFor("refs/heads/main")
	require.NoError(t, err)
	assert.Greater(t, len(logs), 0, "expected reflog entries for refs/heads/main")

	// Entries should be newest first.
	if len(logs) > 1 {
		assert.GreaterOrEqual(t, logs[0].UpdateIndex, logs[1].UpdateIndex)
	}
}
