package index

import (
	"bytes"
	"crypto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func requireGitAtLeast(t *testing.T, wantMajor, wantMinor int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found in PATH")
	}
	out, err := gitenv.Command("git", "version").Output()
	if err != nil {
		t.Skipf("cannot run git version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Skipf("cannot parse git version %q", out)
	}
	parts := strings.SplitN(fields[2], ".", 3)
	major, errMajor := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if errMajor != nil || major < wantMajor || (major == wantMajor && minor < wantMinor) {
		t.Skipf("git %s is older than %d.%d", fields[2], wantMajor, wantMinor)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	require.NoError(t, cmd.Err)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com",
		"GIT_COMMITTER_NAME=c", "GIT_COMMITTER_EMAIL=c@example.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return string(out)
}

func TestGitSHA256IndexRoundTrip(t *testing.T) {
	t.Parallel()
	// SHA-256 repositories need git 2.29.
	requireGitAtLeast(t, 2, 29)

	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "--object-format=sha256")
	for _, f := range []string{"a", "b", filepath.Join("dir", "c")} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644))
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("changed"), 0o644))
	git(t, dir, "add", "a")
	want := git(t, dir, "ls-files", "--stage")

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
	assert.Equal(t, want, got.String())

	require.Error(t, NewDecoder(bytes.NewReader(raw)).Decode(&Index{}))

	var buf bytes.Buffer
	require.NoError(t, NewEncoder(&buf, WithObjectFormat(hash.SHA256)).Encode(idx))
	require.NoError(t, os.WriteFile(indexPath, buf.Bytes(), 0o644))
	assert.Equal(t, want, git(t, dir, "ls-files", "--stage"))
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
