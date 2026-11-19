package index

import (
	"bytes"
	"crypto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func TestGitSHA256IndexRoundTrip(t *testing.T) {
	t.Parallel()
	// SHA-256 repositories need git 2.29.
	gitenv.RequireAtLeast(t, 2, 29)

	dir := t.TempDir()
	gitenv.Run(t, dir, "init", "--quiet", "--object-format=sha256")
	for _, f := range []string{"a", "b", filepath.Join("dir", "c")} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644))
	}
	gitenv.Run(t, dir, "add", ".")
	gitenv.Run(t, dir, "commit", "--quiet", "-m", "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("changed"), 0o644))
	gitenv.Run(t, dir, "add", "a")
	want := gitenv.Run(t, dir, "ls-files", "--stage")

	indexPath := filepath.Join(dir, ".git", "index")
	raw, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	idx := &Index{}
	require.NoError(t, NewDecoder(bytes.NewReader(raw), WithObjectFormat(hash.SHA256)).Decode(idx))
	require.Len(t, idx.Entries, 3)
	var got strings.Builder
	for _, e := range idx.Entries {
		assert.Equal(t, hash.SHA256, e.Hash.Format())
		got.WriteString(strconv.FormatUint(uint64(e.Mode), 8) + " " + e.Hash.String() + " " +
			strconv.Itoa(int(e.Stage)) + "\t" + e.Name + "\n")
	}
	assert.Equal(t, want, strings.TrimSpace(got.String()))

	require.Error(t, NewDecoder(bytes.NewReader(raw)).Decode(&Index{}))

	var buf bytes.Buffer
	require.NoError(t, NewEncoder(&buf, WithObjectFormat(hash.SHA256)).Encode(idx))
	require.NoError(t, os.WriteFile(indexPath, buf.Bytes(), 0o644))
	assert.Equal(t, want, gitenv.Run(t, dir, "ls-files", "--stage"))
}

func TestUnsupportedObjectFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := NewEncoder(&buf, WithObjectFormat("md5")).Encode(&Index{Version: 2})
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	assert.Zero(t, buf.Len())

	require.NoError(t, NewEncoder(&buf).Encode(&Index{Version: 2}))
	err = NewDecoder(bytes.NewReader(buf.Bytes()), WithObjectFormat("md5")).Decode(&Index{})
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
}

func formatOf(h crypto.Hash) hash.ObjectFormat {
	if h == crypto.SHA256 {
		return hash.SHA256
	}
	return hash.SHA1
}
