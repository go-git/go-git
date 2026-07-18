package git

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// TestForceCheckoutReplacesLeadingSymlink covers the symlink-mask bypass
// (CVE-2021-21300 class). A symlink "s" pointing at a dangerous directory
// is present in the worktree, planted by an attacker or left by an earlier
// checkout step, and the commit being force-checked-out writes a file at
// "s/<leaf>". That path string is innocent (no ".git" or ".." component),
// so string validation alone lets it through. Following the symlink would
// let the write escape the worktree.
//
// Matching upstream Git's create_directories, a force checkout must remove
// the blocking symlink and materialise a real directory, writing the file
// safely inside the worktree. Each case asserts the checkout succeeds, the
// dangerous target is untouched, and the file lands in the worktree
// instead.
func TestForceCheckoutReplacesLeadingSymlink(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping symlink-mask path validation test in short mode")
	}
	t.Parallel()

	const exploit = "exploit"

	cases := []struct {
		name string
		// setup returns the symlink target (the on-disk contents of the
		// planted symlink "s"), the tree path the attack commit writes,
		// and the on-disk location that path resolves to if "s" is
		// followed.
		setup func(t *testing.T, repoDir string) (linkTarget, entryPath, escapePath string)
	}{
		{
			name: "masks .git config",
			setup: func(_ *testing.T, repoDir string) (string, string, string) {
				return ".git", "s/config", filepath.Join(repoDir, ".git", "config")
			},
		},
		{
			name: "masks .git hook",
			setup: func(_ *testing.T, repoDir string) (string, string, string) {
				return ".git", "s/hooks/pre-commit", filepath.Join(repoDir, ".git", "hooks", "pre-commit")
			},
		},
		{
			name: "masks parent dir",
			setup: func(_ *testing.T, repoDir string) (string, string, string) {
				return "..", "s/escape", filepath.Join(filepath.Dir(repoDir), "escape")
			},
		},
		{
			name: "masks absolute external dir",
			setup: func(t *testing.T, _ string) (string, string, string) {
				escape := t.TempDir()
				return escape, "s/escape", filepath.Join(escape, "escape")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			r, err := PlainInit(dir, false)
			require.NoError(t, err)

			w, err := r.Worktree()
			require.NoError(t, err)

			require.NoError(t, util.WriteFile(w.Filesystem, "README", []byte("init"), 0o644))
			_, err = w.Add("README")
			require.NoError(t, err)

			initHash, err := w.Commit("initial commit\n", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)
			initCommit, err := r.CommitObject(initHash)
			require.NoError(t, err)

			linkTarget, entryPath, escapePath := tc.setup(t, dir)

			attack := buildCommitWithEntry(t, r.Storer, initCommit, initHash, entryPath, filemode.Regular)

			// Plant the masking symlink in the worktree. This models the
			// pre-existing symlink an attacker relies on.
			if err := w.Filesystem.Symlink(linkTarget, "s"); err != nil {
				if isSymlinkWindowsNonAdmin(err) {
					t.Skipf("symlink creation requires elevated privileges: %v", err)
				}
				require.NoError(t, err)
			}

			// Upstream git checkout -f succeeds here by replacing "s".
			require.NoError(t, w.Checkout(&CheckoutOptions{Hash: attack.Hash, Force: true}),
				"force checkout should replace symlink %q -> %q, not fail", entryPath, linkTarget)

			// The dangerous target must not have been followed.
			if data, readErr := os.ReadFile(escapePath); readErr == nil {
				require.NotEqual(t, exploit, string(data),
					"force checkout wrote through symlink %q -> %q to %s", entryPath, linkTarget, escapePath)
			}

			// The blocking symlink is gone and the file lands safely in the
			// worktree instead.
			fi, err := os.Lstat(filepath.Join(dir, "s"))
			require.NoError(t, err)
			require.Zero(t, fi.Mode()&os.ModeSymlink, "leading symlink %q must be replaced by a real directory", "s")

			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(entryPath)))
			require.NoError(t, err, "checked-out file must exist inside the worktree")
			require.Equal(t, exploit, string(data))
		})
	}
}

// TestWorktreeFilesystemRejectsSymlinkTraversal pins the leading-path
// symlink invariant at the wrapper boundary. Every operation on "s/<name>"
// must be refused while "s" is an existing symlink, so no worktree code
// path, read or write, can follow the link out of the worktree. The path
// string is innocent, so validPath alone would allow it. validNoLeadingSymlink
// is what catches it. Chroot is additionally refused when the final
// component is itself a symlink, the "valid path, wrong target" case that
// Submodule.Repository relies on.
//
// It runs against both memfs and osfs so real on-disk symlink semantics
// are exercised alongside the pure abstraction.
func TestWorktreeFilesystemRejectsSymlinkTraversal(t *testing.T) {
	t.Parallel()

	const wantSubstr = "is a symlink"

	backends := []struct {
		name   string
		makeFS func(t *testing.T) billy.Filesystem
	}{
		{"memfs", func(*testing.T) billy.Filesystem { return memfs.New() }},
		{"osfs", func(t *testing.T) billy.Filesystem { return osfs.New(t.TempDir()) }},
	}

	// symlinkFS wraps a fresh backend filesystem with "s" and "a/b/s"
	// planted as symlinks. Each subtest gets its own, so the parallel
	// subtests never share filesystem state.
	symlinkFS := func(t *testing.T, makeFS func(*testing.T) billy.Filesystem) *worktreeFilesystem {
		t.Helper()
		base := makeFS(t)
		require.NoError(t, base.MkdirAll("elsewhere", 0o755))
		require.NoError(t, base.MkdirAll("a/b", 0o755))
		if err := base.Symlink("elsewhere", "s"); err != nil {
			if isSymlinkWindowsNonAdmin(err) {
				t.Skipf("symlink creation requires elevated privileges: %v", err)
			}
			require.NoError(t, err)
		}
		require.NoError(t, base.Symlink("elsewhere", "a/b/s"))
		return newWorktreeFilesystem(base, false, false)
	}

	for _, bk := range backends {
		t.Run(bk.name, func(t *testing.T) {
			t.Parallel()

			for _, p := range []string{"s/file", "s/deeper/file", "a/b/s/file"} {
				t.Run(p, func(t *testing.T) {
					t.Parallel()
					fs := symlinkFS(t, bk.makeFS)

					_, err := fs.Create(p)
					assert.ErrorContains(t, err, wantSubstr, "Create should reject %q", p)

					_, err = fs.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
					assert.ErrorContains(t, err, wantSubstr, "OpenFile should reject %q", p)

					_, err = fs.Open(p)
					assert.ErrorContains(t, err, wantSubstr, "Open should reject read through symlink %q", p)

					err = fs.MkdirAll(p, 0o755)
					assert.ErrorContains(t, err, wantSubstr, "MkdirAll should reject %q", p)

					err = fs.Symlink("target", p)
					assert.ErrorContains(t, err, wantSubstr, "Symlink should reject %q", p)

					err = fs.Rename("readme.md", p)
					assert.ErrorContains(t, err, wantSubstr, "Rename should reject destination %q", p)

					err = fs.Remove(p)
					assert.ErrorContains(t, err, wantSubstr, "Remove should reject %q", p)
				})
			}

			t.Run("Chroot through leading symlink", func(t *testing.T) {
				t.Parallel()
				fs := symlinkFS(t, bk.makeFS)
				_, err := fs.Chroot("s/sub")
				assert.ErrorContains(t, err, wantSubstr, "Chroot should reject a leading symlink")
			})

			// The submodule-scoping case: Submodule.Repository chroots into
			// the tree-controlled submodule path. A symlink as the final
			// component must not redirect the scope out of the worktree.
			t.Run("Chroot onto symlink target", func(t *testing.T) {
				t.Parallel()
				fs := symlinkFS(t, bk.makeFS)
				_, err := fs.Chroot("s")
				assert.ErrorContains(t, err, wantSubstr, "Chroot should reject a symlink as the final component")
			})
		})
	}
}

// TestCheckoutReplacesBlockingFinalSymlink pins the final-component case at
// the checkout materialisation boundary: writing a tracked regular file at
// a path that already exists as a symlink must replace the symlink, not
// follow it and overwrite the link's target. Runs on both memfs and osfs.
func TestCheckoutReplacesBlockingFinalSymlink(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, base billy.Filesystem) {
		t.Helper()

		w := &Worktree{
			Filesystem: newWorktreeFilesystem(base, defaultProtectNTFS(), defaultProtectHFS()),
		}

		require.NoError(t, util.WriteFile(base, "target.txt", []byte("keep"), 0o644))
		if err := base.Symlink("target.txt", "tracked.txt"); err != nil {
			if isSymlinkWindowsNonAdmin(err) {
				t.Skipf("symlink creation requires elevated privileges: %v", err)
			}
			require.NoError(t, err)
		}

		blobObj := &plumbing.MemoryObject{}
		blobObj.SetType(plumbing.BlobObject)
		_, err := blobObj.Write([]byte("replacement"))
		require.NoError(t, err)
		blob, err := object.DecodeBlob(blobObj)
		require.NoError(t, err)

		require.NoError(t, w.checkoutFile(object.NewFile("tracked.txt", filemode.Regular, blob)))

		got, err := util.ReadFile(base, "tracked.txt")
		require.NoError(t, err)
		assert.Equal(t, "replacement", string(got))

		// The symlink target must be untouched.
		target, err := util.ReadFile(base, "target.txt")
		require.NoError(t, err)
		assert.Equal(t, "keep", string(target))

		fi, err := base.Lstat("tracked.txt")
		require.NoError(t, err)
		assert.Zero(t, fi.Mode()&os.ModeSymlink, "tracked.txt must be a regular file, not a symlink")
	}

	t.Run("memfs", func(t *testing.T) { t.Parallel(); run(t, memfs.New()) })
	t.Run("osfs", func(t *testing.T) { t.Parallel(); run(t, osfs.New(t.TempDir())) })
}

// writeBlob stores content as a blob object and returns its hash.
func writeBlob(t *testing.T, s storer.Storer, content []byte) plumbing.Hash {
	t.Helper()

	obj := s.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(int64(len(content)))
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	hash, err := s.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

// storeRawTree writes a tree object to s by assembling the raw
// `<mode> SP <name> NUL <hash>` bytes for each entry, bypassing
// Tree.Encode's validation so tests can plant otherwise-refused names.
func storeRawTree(t *testing.T, s storer.Storer, entries []object.TreeEntry) plumbing.Hash {
	t.Helper()

	var buf bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&buf, "%o %s", e.Mode, e.Name)
		buf.WriteByte(0)
		buf.Write(e.Hash[:])
	}

	obj := s.NewEncodedObject()
	obj.SetType(plumbing.TreeObject)
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = w.Write(buf.Bytes())
	require.NoError(t, err)
	require.NoError(t, w.Close())

	hash, err := s.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

// buildCommitWithEntry builds a commit on top of parent whose tree is the
// parent tree plus a single entry at filePath (nested trees are created
// for each intermediate directory), with "exploit" as the blob content.
func buildCommitWithEntry(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, filePath string, leafMode filemode.FileMode) *object.Commit {
	t.Helper()

	leafHash := writeBlob(t, s, []byte("exploit"))

	parts := strings.Split(filePath, "/")
	for i := len(parts) - 1; i >= 1; i-- {
		entry := object.TreeEntry{Name: parts[i], Mode: leafMode, Hash: leafHash}
		leafHash = storeRawTree(t, s, []object.TreeEntry{entry})
		leafMode = filemode.Dir
	}

	return buildCommitWithEntries(t, s, parent, parentHash,
		[]object.TreeEntry{{Name: parts[0], Mode: leafMode, Hash: leafHash}},
		"bad path: "+filePath+"\n")
}

// buildCommitWithEntries builds a commit on top of parent whose tree is
// the parent tree plus the given extra entries.
func buildCommitWithEntries(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, extra []object.TreeEntry, message string) *object.Commit {
	t.Helper()

	parentTree, err := parent.Tree()
	require.NoError(t, err)

	entries := make([]object.TreeEntry, len(parentTree.Entries), len(parentTree.Entries)+len(extra))
	copy(entries, parentTree.Entries)
	entries = append(entries, extra...)
	sort.Sort(object.TreeEntrySorter(entries))
	rootHash := storeRawTree(t, s, entries)

	commit := &object.Commit{
		Author:       *defaultSignature(),
		Committer:    *defaultSignature(),
		Message:      message,
		TreeHash:     rootHash,
		ParentHashes: []plumbing.Hash{parentHash},
	}
	commitObj := s.NewEncodedObject()
	require.NoError(t, commit.Encode(commitObj))
	commitHash, err := s.SetEncodedObject(commitObj)
	require.NoError(t, err)

	result, err := object.GetCommit(s, commitHash)
	require.NoError(t, err)
	return result
}
