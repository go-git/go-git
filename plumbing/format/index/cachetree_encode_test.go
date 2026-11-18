package index

import (
	"bytes"
	"crypto"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
)

func TestEncodeWritesTheCacheTreeGitWrote(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	idx := readIndex(t, dir)
	require.NotNil(t, idx.Cache)
	want := append([]TreeEntry(nil), idx.Cache.Entries...)

	writeIndex(t, dir, idx)

	back := readIndex(t, dir)
	require.NotNil(t, back.Cache)
	assert.Equal(t, want, back.Cache.Entries)

	runGit(t, dir, "fsck", "--strict")
	assert.Empty(t, runGit(t, dir, "status", "--porcelain"))
	assert.Empty(t, runGit(t, dir, "diff-index", "--cached", "HEAD"))
	assert.Equal(t, runGit(t, dir, "rev-parse", "HEAD^{tree}"), runGit(t, dir, "write-tree"))
}

func TestEncodedCacheTreeIsByteIdenticalToGits(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	gitBytes, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	require.NoError(t, err)

	writeIndex(t, dir, readIndex(t, dir))
	goBytes, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	require.NoError(t, err)

	ext := func(b []byte) []byte {
		i := bytes.Index(b, []byte("TREE"))
		require.GreaterOrEqual(t, i, 0)
		return b[i : len(b)-20]
	}
	assert.Equal(t, ext(gitBytes), ext(goBytes))
}

func TestEncodeDoesNotWriteAStaleCacheTree(t *testing.T) {
	t.Parallel()

	dir := repoWithCacheTree(t)
	idx := readIndex(t, dir)

	changed, err := idx.Entry("a/b/c/h")
	require.NoError(t, err)
	blob := strings.TrimSpace(runGitInput(t, dir, "changed\n", "hash-object", "-w", "--stdin"))
	changed.Hash = plumbing.NewHash(blob)

	writeIndex(t, dir, idx)

	back := readIndex(t, dir)
	assert.Equal(t, []string{
		":invalid", "a/:invalid", "a/b/:invalid", "a/b/c/:invalid", "d/:valid", "z/:valid", "z/y/:valid", "z/y/x/:valid", "z/y/x/w/:valid",
	}, fullPaths(back))

	runGit(t, dir, "fsck", "--strict")
	want := runGit(t, dir, "write-tree")
	probe := t.TempDir()
	require.NoError(t, os.CopyFS(probe, os.DirFS(dir)))
	runGit(t, probe, "read-tree", "--empty")
	runGit(t, probe, "read-tree", want)
	assert.Equal(t, want, runGit(t, probe, "write-tree"))
	assert.NotEqual(t, runGit(t, dir, "rev-parse", "HEAD^{tree}"), want)
}

func TestEncodeWritesTheCacheTreeOfASHA256Repository(t *testing.T) {
	t.Parallel()

	if _, err := gitenv.Command("git", "--version").CombinedOutput(); err != nil {
		t.Skip("git not available")
	}
	if !gitAtLeast(2, 29) {
		t.Skip("git init --object-format needs Git 2.29")
	}
	dir := t.TempDir()
	if out, err := gitenv.Command("git", "-C", dir, "init", "-q", "--object-format=sha256").CombinedOutput(); err != nil {
		t.Skipf("git cannot create a SHA-256 repository: %s", out)
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "f"), []byte("f\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top"), []byte("top\n"), 0o644))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "c")

	path := filepath.Join(dir, ".git", "index")
	gitBytes, err := os.ReadFile(path)
	require.NoError(t, err)

	idx := &Index{}
	require.NoError(t, NewDecoder(bytes.NewReader(gitBytes), crypto.SHA256.New()).Decode(idx))
	require.NotNil(t, idx.Cache)

	var buf bytes.Buffer
	require.NoError(t, NewEncoder(&buf, crypto.SHA256.New()).Encode(idx))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))

	runGit(t, dir, "fsck", "--strict")
	assert.Equal(t, runGit(t, dir, "rev-parse", "HEAD^{tree}"), runGit(t, dir, "write-tree"))
	assert.Empty(t, runGit(t, dir, "status", "--porcelain"))

	idx.Cache = nil
	root := plumbing.NewHash(runGit(t, dir, "rev-parse", "HEAD^{tree}"))
	assert.True(t, idx.FillCache(root), "FillCache for a SHA-256 tree")
}

func writeIndex(t *testing.T, dir string, idx *Index) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, NewEncoder(&buf, crypto.SHA1.New()).Encode(idx))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "index"), buf.Bytes(), 0o644))
}

// gitAtLeast reports whether the local `git` is at least the given version.
func gitAtLeast(major, minor int) bool {
	out, err := gitenv.Command("git", "--version").Output()
	if err != nil {
		return false
	}
	var maj, mnr int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d", &maj, &mnr); err != nil {
		return false
	}
	return maj > major || (maj == major && mnr >= minor)
}
