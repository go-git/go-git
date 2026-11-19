package commitgraph_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func sha256ID(t *testing.T, b byte) plumbing.Hash {
	t.Helper()
	id, ok := plumbing.FromHex(strings.Repeat(fmt.Sprintf("%02x", b), hash.SHA256Size))
	require.True(t, ok)
	return id
}

func sha256Graph(t *testing.T) (*commitgraph.MemoryIndex, []plumbing.Hash) {
	t.Helper()
	root, left, right, merge := sha256ID(t, 0x10), sha256ID(t, 0x20), sha256ID(t, 0x30), sha256ID(t, 0x40)
	mem := commitgraph.NewMemoryIndex()
	mem.Add(root, &commitgraph.CommitData{
		TreeHash: sha256ID(t, 0xa0), Generation: 1, GenerationV2: 100, When: time.Unix(100, 0),
	})
	mem.Add(left, &commitgraph.CommitData{
		TreeHash: sha256ID(t, 0xa1), ParentHashes: []plumbing.Hash{root},
		Generation: 2, GenerationV2: 200, When: time.Unix(200, 0),
	})
	mem.Add(right, &commitgraph.CommitData{
		TreeHash: sha256ID(t, 0xa2), ParentHashes: []plumbing.Hash{root},
		Generation: 2, GenerationV2: 300, When: time.Unix(300, 0),
	})
	mem.Add(merge, &commitgraph.CommitData{
		TreeHash: sha256ID(t, 0xa3), ParentHashes: []plumbing.Hash{left, right},
		Generation: 3, GenerationV2: 400, When: time.Unix(400, 0),
	})
	return mem, []plumbing.Hash{root, left, right, merge}
}

func TestSHA256RoundTrip(t *testing.T) {
	t.Parallel()

	mem, ids := sha256Graph(t)
	var buf bytes.Buffer
	require.NoError(t, commitgraph.NewEncoder(&buf, commitgraph.WithObjectFormat(hash.SHA256)).Encode(mem))
	assert.Equal(t, byte(2), buf.Bytes()[5], "hash version")

	for _, opts := range [][]commitgraph.Option{nil, {commitgraph.WithObjectFormat(hash.SHA256)}} {
		idx, err := commitgraph.OpenFileIndex(discardCloseReader{bytes.NewReader(buf.Bytes())}, opts...)
		require.NoError(t, err)

		assert.Equal(t, ids, idx.Hashes())
		for _, id := range ids {
			want, err := mem.GetCommitDataByIndex(mustIndex(t, mem, id))
			require.NoError(t, err)

			i, err := idx.GetIndexByHash(id)
			require.NoError(t, err)
			got, err := idx.GetCommitDataByIndex(i)
			require.NoError(t, err)
			assert.Equal(t, want.TreeHash, got.TreeHash)
			assert.Equal(t, hash.SHA256, got.TreeHash.Format())
			if len(want.ParentHashes) == 0 {
				assert.Empty(t, got.ParentHashes)
			} else {
				assert.Equal(t, want.ParentHashes, got.ParentHashes)
			}
			assert.Equal(t, want.Generation, got.Generation)
			assert.Equal(t, want.GenerationV2, got.GenerationV2)
			assert.Equal(t, want.When.Unix(), got.When.Unix())

			h, err := idx.GetHashByIndex(i)
			require.NoError(t, err)
			assert.Equal(t, id, h)
		}
		require.NoError(t, idx.Close())
	}
}

func mustIndex(t *testing.T, idx commitgraph.Index, id plumbing.Hash) uint32 {
	t.Helper()
	i, err := idx.GetIndexByHash(id)
	require.NoError(t, err)
	return i
}

func TestOpenFileIndexRejectsObjectFormatMismatch(t *testing.T) {
	t.Parallel()

	sha256Mem, _ := sha256Graph(t)
	var sha256Buf bytes.Buffer
	require.NoError(t, commitgraph.NewEncoder(&sha256Buf, commitgraph.WithObjectFormat(hash.SHA256)).Encode(sha256Mem))

	sha1Mem := commitgraph.NewMemoryIndex()
	sha1Mem.Add(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), &commitgraph.CommitData{
		TreeHash: plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), Generation: 1,
	})
	var sha1Buf bytes.Buffer
	require.NoError(t, commitgraph.NewEncoder(&sha1Buf).Encode(sha1Mem))

	tests := []struct {
		name   string
		data   []byte
		format hash.ObjectFormat
	}{
		{"sha256 graph read as sha1", sha256Buf.Bytes(), hash.SHA1},
		{"sha1 graph read as sha256", sha1Buf.Bytes(), hash.SHA256},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := commitgraph.OpenFileIndex(discardCloseReader{bytes.NewReader(tc.data)},
				commitgraph.WithObjectFormat(tc.format))
			require.ErrorIs(t, err, commitgraph.ErrUnsupportedHash)
		})
	}
}

func TestOpenFileIndexRejectsUnknownHashVersion(t *testing.T) {
	t.Parallel()

	mem, _ := sha256Graph(t)
	var buf bytes.Buffer
	require.NoError(t, commitgraph.NewEncoder(&buf, commitgraph.WithObjectFormat(hash.SHA256)).Encode(mem))
	raw := buf.Bytes()
	raw[5] = 3

	_, err := commitgraph.OpenFileIndex(discardCloseReader{bytes.NewReader(raw)})
	require.ErrorIs(t, err, commitgraph.ErrUnsupportedHash)
}

func TestEncoderRejectsIDsOfAnotherFormat(t *testing.T) {
	t.Parallel()

	mem, _ := sha256Graph(t)
	err := commitgraph.NewEncoder(&bytes.Buffer{}).Encode(mem)
	require.ErrorIs(t, err, commitgraph.ErrUnsupportedHash)

	sha1Mem := commitgraph.NewMemoryIndex()
	sha1Mem.Add(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), &commitgraph.CommitData{
		TreeHash: plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), Generation: 1,
	})
	err = commitgraph.NewEncoder(&bytes.Buffer{}, commitgraph.WithObjectFormat(hash.SHA256)).Encode(sha1Mem)
	require.ErrorIs(t, err, commitgraph.ErrUnsupportedHash)
}

func TestEncoderRejectsUnsupportedObjectFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := commitgraph.NewEncoder(&buf, commitgraph.WithObjectFormat("md5")).Encode(commitgraph.NewMemoryIndex())
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	assert.Zero(t, buf.Len())
}

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
		"GIT_AUTHOR_DATE=1700000000 +0000", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// gitSHA256Repo creates a SHA-256 repository holding a root commit, two
// children of it and their merge.
func gitSHA256Repo(t *testing.T) string {
	t.Helper()
	// SHA-256 repositories need git 2.29.
	requireGitAtLeast(t, 2, 29)

	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "--object-format=sha256", "--initial-branch=main")
	for _, f := range []string{"root", "left"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644))
		git(t, dir, "add", f)
		git(t, dir, "commit", "--quiet", "-m", f)
		if f == "root" {
			git(t, dir, "branch", "side")
		}
	}
	git(t, dir, "checkout", "--quiet", "side")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "right"), []byte("right"), 0o644))
	git(t, dir, "add", "right")
	git(t, dir, "commit", "--quiet", "-m", "right")
	git(t, dir, "checkout", "--quiet", "main")
	git(t, dir, "merge", "--quiet", "--no-edit", "side")
	return dir
}

func revParse(t *testing.T, dir, rev string) plumbing.Hash {
	t.Helper()
	id, ok := plumbing.FromHex(git(t, dir, "rev-parse", rev))
	require.True(t, ok)
	return id
}

func TestOpenGitWrittenSHA256Graph(t *testing.T) {
	t.Parallel()

	for _, split := range []bool{false, true} {
		t.Run("split="+strconv.FormatBool(split), func(t *testing.T) {
			t.Parallel()
			dir := gitSHA256Repo(t)
			args := []string{"commit-graph", "write", "--reachable"}
			if split {
				args = append(args, "--split")
			}
			git(t, dir, args...)

			for _, opts := range [][]commitgraph.Option{nil, {commitgraph.WithObjectFormat(hash.SHA256)}} {
				idx, err := commitgraph.OpenChainOrFileIndex(osfs.New(filepath.Join(dir, ".git")), opts...)
				require.NoError(t, err)

				assert.Len(t, idx.Hashes(), 4)
				head := revParse(t, dir, "HEAD")
				i, err := idx.GetIndexByHash(head)
				require.NoError(t, err)
				data, err := idx.GetCommitDataByIndex(i)
				require.NoError(t, err)
				assert.Equal(t, revParse(t, dir, "HEAD^{tree}"), data.TreeHash)
				assert.Equal(t, []plumbing.Hash{revParse(t, dir, "HEAD^1"), revParse(t, dir, "HEAD^2")}, data.ParentHashes)
				assert.Equal(t, uint64(3), data.Generation)
				require.NoError(t, idx.Close())
			}

			_, err := commitgraph.OpenChainOrFileIndex(osfs.New(filepath.Join(dir, ".git")),
				commitgraph.WithObjectFormat(hash.SHA1))
			require.ErrorIs(t, err, commitgraph.ErrUnsupportedHash)
		})
	}
}

func TestGoGitWrittenSHA256GraphPassesGitVerify(t *testing.T) {
	t.Parallel()

	dir := gitSHA256Repo(t)
	git(t, dir, "commit-graph", "write", "--reachable")
	graphPath := filepath.Join(dir, ".git", "objects", "info", "commit-graph")
	gitGraph, err := os.ReadFile(graphPath)
	require.NoError(t, err)

	idx, err := commitgraph.OpenFileIndex(discardCloseReader{bytes.NewReader(gitGraph)})
	require.NoError(t, err)
	mem := commitgraph.NewMemoryIndex()
	for i, id := range idx.Hashes() {
		data, err := idx.GetCommitDataByIndex(uint32(i))
		require.NoError(t, err)
		mem.Add(id, data)
	}
	require.NoError(t, idx.Close())

	var buf bytes.Buffer
	require.NoError(t, commitgraph.NewEncoder(&buf, commitgraph.WithObjectFormat(hash.SHA256)).Encode(mem))
	assert.Equal(t, gitGraph, buf.Bytes())

	require.NoError(t, os.Chmod(graphPath, 0o644))
	require.NoError(t, os.WriteFile(graphPath, buf.Bytes(), 0o644))
	git(t, dir, "commit-graph", "verify")
}
