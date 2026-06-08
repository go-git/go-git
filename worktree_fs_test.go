package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestValidPath(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{".git", true},
		{".git/b", true},
		{".git\\b", true},
		{"git~1", true},
		{"a/../b", true},
		{"a\\..\\b", true},
		{"/", true},
		{"", true},
		{".gitmodules", false},
		{".gitignore", false},
		{"a..b", false},
		{".", true},
		{"a/.git/b", true},
		{"a\\.git\\b", true},
		{"a/.git", false},
		{"a\\.git", false},
		{"a\x01b", true},     // explicit byte-oriented control-char rejection
		{"foo\x7fbar", true}, // DEL byte
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(tc.path)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWorktreeFilesystemRejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	badPaths := []string{
		".git/config",
		".git/objects/pack/file",
		"git~1/HEAD",
		"../escape",
		"a/../../etc/passwd",
	}

	for _, p := range badPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			_, err := fs.Create(p)
			assert.Error(t, err, "Create should reject %q", p)

			_, err = fs.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			assert.Error(t, err, "OpenFile should reject %q", p)

			err = fs.Remove(p)
			assert.Error(t, err, "Remove should reject %q", p)

			err = fs.MkdirAll(p, 0o755)
			assert.Error(t, err, "MkdirAll should reject %q", p)

			err = fs.Symlink("target", p)
			assert.Error(t, err, "Symlink should reject %q", p)
		})
	}

	for _, p := range badPaths {
		t.Run("Rename/from/"+p, func(t *testing.T) {
			t.Parallel()
			err := fs.Rename(p, "dst")
			assert.Error(t, err, "Rename should reject from=%q", p)
		})
		t.Run("Rename/to/"+p, func(t *testing.T) {
			t.Parallel()
			err := fs.Rename("src", p)
			assert.Error(t, err, "Rename should reject to=%q", p)
		})
	}
}

func TestWorktreeFilesystemAllowsValidPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	validPaths := []string{
		"readme.md",
		"src/main.go",
		".gitignore",
	}

	for _, p := range validPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			f, err := fs.Create(p)
			require.NoError(t, err, "Create should allow %q", p)
			require.NoError(t, f.Close())

			err = fs.Remove(p)
			assert.NoError(t, err, "Remove should allow %q", p)
		})
	}
}

// TestWorktreeFilesystemMkdirAllRootIsNoop locks in the contract that
// MkdirAll on a root-equivalent path is a silent no-op against the
// wrapper. validPath itself still rejects "", ".", and "/" (see
// TestValidPath), but MkdirAll specifically tolerates them because
// "ensure the root exists" is always trivially satisfied.
func TestWorktreeFilesystemMkdirAllRootIsNoop(t *testing.T) {
	t.Parallel()

	rootPaths := []string{"", ".", "/"}
	for _, p := range rootPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			mfs := memfs.New()
			fs := newWorktreeFilesystem(mfs, true, true)

			require.NoError(t, fs.MkdirAll(p, 0o755))

			entries, err := mfs.ReadDir("/")
			require.NoError(t, err)
			assert.Empty(t, entries, "MkdirAll(%q) must not materialise a directory entry", p)
		})
	}
}

func TestWorktreeFilesystemReturnsWorktreeFilesystem(t *testing.T) {
	t.Parallel()

	t.Run("via Repository.Worktree", func(t *testing.T) {
		t.Parallel()

		mfs := memfs.New()
		r, err := Init(memory.NewStorage(), WithWorkTree(mfs))
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		w, err := r.Worktree()
		require.NoError(t, err)

		assert.Equal(t, mfs, w.Filesystem())

		_, err = w.filesystem.Create(".git/file")
		assert.Error(t, err, "Create through worktreeFilesystem should reject .git paths")
	})

	t.Run("via struct literal", func(t *testing.T) {
		t.Parallel()

		mfs := memfs.New()
		w := &Worktree{filesystem: newWorktreeFilesystem(mfs, false, false)}

		assert.Equal(t, mfs, w.Filesystem())

		_, err := w.filesystem.Create(".git/file")
		assert.Error(t, err)
	})
}

// assertOpsRejected exercises the read/write surface of the wrapper
// against a dangerous path and asserts every operation is rejected. Used
// across the symlink tests to demonstrate that the wrapper's protections
// hold no matter how the call site got there.
func assertOpsRejected(t *testing.T, fs *worktreeFilesystem, p string) {
	t.Helper()

	_, err := fs.Open(p)
	assert.ErrorContains(t, err, "open:", "Open should reject %q", p)

	_, err = fs.Create(p)
	assert.ErrorContains(t, err, "create:", "Create should reject %q", p)

	_, err = fs.OpenFile(p, os.O_RDWR, 0o644)
	assert.ErrorContains(t, err, "openfile:", "OpenFile should reject %q", p)

	err = fs.Remove(p)
	assert.ErrorContains(t, err, "remove:", "Remove should reject %q", p)

	_, err = fs.Lstat(p)
	assert.ErrorContains(t, err, "lstat:", "Lstat should reject %q", p)

	_, err = fs.Readlink(p)
	assert.ErrorContains(t, err, "readlink:", "Readlink should reject %q", p)
}

func TestWorktreeFilesystemSymlinkRejectsDangerousLinkNames(t *testing.T) {
	t.Parallel()

	badPaths := []string{
		".git",
		".git/config",
		".git/hooks/pre-commit",
		"git~1/HEAD",
		"../escape",
		"a/../../etc/passwd",
	}

	for _, p := range badPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			fs := newWorktreeFilesystem(memfs.New(), false, false)

			err := fs.Symlink("safe-target.txt", p)
			assert.ErrorContains(t, err, "symlink:", "Symlink should reject link name %q", p)

			assertOpsRejected(t, fs, p)
		})
	}
}

func TestWorktreeFilesystemSymlinkAllowsValidLink(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	require.NoError(t, fs.Symlink("target.txt", "link"))

	got, err := fs.Readlink("link")
	require.NoError(t, err)
	assert.Equal(t, "target.txt", got)

	assertOpsRejected(t, fs, ".git/config")
}

func TestWorktreeFilesystemSymlinkAllowsArbitraryTargets(t *testing.T) {
	t.Parallel()

	targets := []string{
		"/etc/passwd",
		"/absolute/path/to/file",
		"../sibling",
		"../../elsewhere",
		"a/../b",
		".git/config",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			fs := newWorktreeFilesystem(memfs.New(), false, false)

			link := "link"
			require.NoError(t, fs.Symlink(target, link))

			got, err := fs.Readlink(link)
			require.NoError(t, err)
			assert.Equal(t, filepath.FromSlash(target), got)
		})
	}
}

func TestWorktreeFilesystemReadlinkValidatesPath(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)
	require.NoError(t, fs.Symlink("target.txt", "good-link"))

	t.Run("rejects bad link path", func(t *testing.T) {
		t.Parallel()
		_, err := fs.Readlink(".git/config")
		assert.ErrorContains(t, err, "readlink:")
	})

	t.Run("allows valid link path", func(t *testing.T) {
		t.Parallel()
		got, err := fs.Readlink("good-link")
		require.NoError(t, err)
		assert.Equal(t, "target.txt", got)
	})

	assertOpsRejected(t, fs, ".git/config")
}

// TestWorktreeFilesystemFollowsSymlinkOnOpen verifies that Open on a
// symlink-named path follows the link via the underlying billy.Filesystem
// for legitimate links, while still rejecting any operation that targets a
// dangerous path directly.
func TestWorktreeFilesystemFollowsSymlinkOnOpen(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	require.NoError(t, util.WriteFile(fs, "data.txt", []byte("hello"), 0o644))
	require.NoError(t, fs.Symlink("data.txt", "alias"))

	f, err := fs.Open("alias")
	require.NoError(t, err)
	defer f.Close()

	buf := make([]byte, 5)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))

	assertOpsRejected(t, fs, ".git/config")
}

// TestWorktreeFilesystemRejectsOpsOnPreExistingDotGitSymlink covers the case
// where a `.git` symlink was placed on the underlying filesystem before the
// wrapper saw it (e.g. a crafted on-disk repository). The wrapper validates
// the path the caller passed, so every operation against the `.git` name is
// refused regardless of what the symlink resolves to.
func TestWorktreeFilesystemRejectsOpsOnPreExistingDotGitSymlink(t *testing.T) {
	t.Parallel()

	mfs := memfs.New()

	require.NoError(t, util.WriteFile(mfs, "real.txt", []byte("data"), 0o644))
	require.NoError(t, mfs.Symlink("real.txt", ".git"))

	fs := newWorktreeFilesystem(mfs, false, false)

	assertOpsRejected(t, fs, ".git")
	assertOpsRejected(t, fs, ".git/config")
}

// assertOpsAllowed verifies the round-trip read/write surface for a path
// the wrapper should accept: write a payload, read it back, and Lstat it.
func assertOpsAllowed(t *testing.T, fs *worktreeFilesystem, p string) {
	t.Helper()

	const payload = "payload"
	require.NoError(t, util.WriteFile(fs, p, []byte(payload), 0o644))

	f, err := fs.Open(p)
	require.NoError(t, err, "Open should accept %q", p)
	t.Cleanup(func() { _ = f.Close() })

	buf := make([]byte, len(payload))
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, payload, string(buf[:n]))

	fi, err := fs.Lstat(p)
	require.NoError(t, err, "Lstat should accept %q", p)
	assert.Equal(t, int64(len(payload)), fi.Size())
}

func TestWorktreeFilesystemAbsolutePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		wantReject bool
	}{
		{"reject /.git", "/.git", true},
		{"reject /.git/config", "/.git/config", true},
		{"reject /.git/objects/pack/file", "/.git/objects/pack/file", true},
		{"reject /git~1/HEAD", "/git~1/HEAD", true},
		{"reject /sub/.git/config", "/sub/.git/config", true},
		{"allow /readme.md", "/readme.md", false},
		{"allow /src/main.go", "/src/main.go", false},
		{"allow /.gitignore", "/.gitignore", false},
		{"allow /submodule/.git", "/submodule/.git", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := newWorktreeFilesystem(memfs.New(), false, false)

			if tc.wantReject {
				assertOpsRejected(t, fs, tc.path)

				err := fs.Symlink("safe-target.txt", tc.path)
				assert.ErrorContains(t, err, "symlink:", "Symlink should reject link %q", tc.path)
				return
			}

			assertOpsAllowed(t, fs, tc.path)
		})
	}
}

// TestCherryPickPathValidationMatchesGit verifies that go-git and upstream
// Git both reject cherry-picking commits that contain dangerous paths.
//
// For each test case, a commit is crafted (via go-git plumbing) in an
// on-disk repository with a tree containing a single bad path. Both go-git
// CherryPick and `git cherry-pick` are run against it. Both must reject.
func TestCherryPickPathValidationMatchesGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping path validation conformance test in short mode")
	}
	t.Parallel()

	tests := []struct {
		name string
		// path is the file path to place in the crafted commit's tree.
		// Nested paths (containing /) are built as nested tree objects.
		path string
		// config overrides to set before running cherry-pick.
		config map[string]string
		// skipGit skips the upstream git cherry-pick check. Used for
		// checks that go-git enforces but upstream git does not on this
		// platform (e.g. reserved device names are only checked by
		// compat/mingw.c, which is not compiled on non-Windows).
		skipGit bool
	}{
		{
			name: ".git at root",
			path: ".git/config",
		},
		{
			name: ".git in subdirectory",
			path: "subdir/.git/config",
		},
		{
			// Final-component .git as a regular blob mimics an attacker
			// trying to overwrite a submodule's gitlink pointer via tree
			// content. Upstream verify_path rejects this at every position
			// and so does pathutil.ValidTreePath, called from
			// Tree.FindEntry. The legitimate submodule shape is the
			// directory entry itself (mode 160000 at "submodule"); a
			// `.git` file inside is a checkout-time artifact, not a tree
			// entry.
			name: "final-component .git in subdirectory",
			path: "submodule/.git",
		},
		{
			name:    "git~1 8.3 short name",
			path:    "git~1/config",
			skipGit: !gitAtLeast(t, 2, 24),
		},
		{
			name: "dot-dot traversal",
			path: "a/../../etc/passwd",
		},
		{
			name: "single dot component",
			path: "a/./b",
		},
		{
			name:   "NTFS trailing space on .git",
			path:   ".git /config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS trailing dot on .git",
			path:   ".git./config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:    "NTFS alternate data stream",
			path:    ".git::$INDEX_ALLOCATION/config",
			config:  map[string]string{"core.protectNTFS": "true"},
			skipGit: !gitAtLeast(t, 2, 24),
		},
		{
			name:    "NTFS reserved device name CON",
			path:    "CON/file",
			config:  map[string]string{"core.protectNTFS": "true"},
			skipGit: runtime.GOOS != "windows",
		},
		{
			name:    "NTFS reserved device name NUL",
			path:    "NUL",
			config:  map[string]string{"core.protectNTFS": "true"},
			skipGit: runtime.GOOS != "windows",
		},
		{
			name:   "HFS+ zero-width character in .git",
			path:   ".g\u200cit/config",
			config: map[string]string{"core.protectHFS": "true"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			r1, err := PlainInit(dir, false)
			require.NoError(t, err)
			defer func() { _ = r1.Close() }()

			w, err := r1.Worktree()
			require.NoError(t, err)

			require.NoError(t, util.WriteFile(w.Filesystem(), "README", []byte("init"), 0o644))
			_, err = w.Add("README")
			require.NoError(t, err)

			initHash, err := w.Commit("initial commit\n", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)

			for k, v := range tc.config {
				gitConfig(t, dir, k, v)
			}

			initCommit, err := r1.CommitObject(initHash)
			require.NoError(t, err)

			badCommit := buildCommitWithEntry(t, r1.Storer, initCommit, initHash, tc.path, filemode.Regular)

			// Re-open so config overrides take effect in the worktreeFilesystem.
			r2, err := PlainOpen(dir)
			require.NoError(t, err)
			defer func() { _ = r2.Close() }()

			w, err = r2.Worktree()
			require.NoError(t, err)

			goGitErr := w.CherryPick(
				&CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true},
				TheirsMergeStrategy, badCommit,
			)
			assert.Error(t, goGitErr, "go-git should reject cherry-pick of %q", tc.path)

			if !tc.skipGit {
				require.NoError(t, w.Reset(&ResetOptions{Commit: initHash, Mode: HardReset}))

				gitErr := gitCherryPick(t, dir, badCommit.Hash.String())
				assert.Error(t, gitErr, "git should reject cherry-pick of %q", tc.path)
			}
		})
	}
}

func buildCommitWithEntry(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, filePath string, leafMode filemode.FileMode) *object.Commit {
	t.Helper()

	content := []byte("exploit")
	blobObj := s.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	blobObj.SetSize(int64(len(content)))
	bw, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = bw.Write(content)
	require.NoError(t, err)
	require.NoError(t, bw.Close())
	blobHash, err := s.SetEncodedObject(blobObj)
	require.NoError(t, err)

	// Build nested tree structure from leaf to root.
	parts := strings.Split(filePath, "/")
	leafHash := blobHash

	for i := len(parts) - 1; i >= 1; i-- {
		entry := object.TreeEntry{Name: parts[i], Mode: leafMode, Hash: leafHash}
		leafHash = storeRawTree(t, s, []object.TreeEntry{entry})
		leafMode = filemode.Dir
	}

	parentTree, err := parent.Tree()
	require.NoError(t, err)

	entries := make([]object.TreeEntry, len(parentTree.Entries), len(parentTree.Entries)+1)
	copy(entries, parentTree.Entries)
	entries = append(entries, object.TreeEntry{
		Name: parts[0],
		Mode: leafMode,
		Hash: leafHash,
	})
	sort.Sort(object.TreeEntrySorter(entries))
	rootHash := storeRawTree(t, s, entries)

	commit := &object.Commit{
		Author:       *defaultSignature(),
		Committer:    *defaultSignature(),
		Message:      "bad path: " + filePath + "\n",
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

// storeRawTree writes a tree object to s by assembling the raw
// `<mode> SP <name> NUL <hash>` bytes for each entry. Tests that plant
// trees containing components like ".git", "..", or HFS+/NTFS variants
// use this helper because Tree.Encode runs Tree.Validate and refuses
// those names — the escape hatch documented on Tree.Encode's godoc.
func storeRawTree(t *testing.T, s storer.Storer, entries []object.TreeEntry) plumbing.Hash {
	t.Helper()

	var buf bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&buf, "%o %s", e.Mode, e.Name)
		buf.WriteByte(0)
		buf.Write(e.Hash.Bytes())
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

func gitConfig(t *testing.T, dir, key, value string) {
	t.Helper()
	cmd := exec.Command("git", "config", key, value)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git config %s %s: %s", key, value, out)
}

// gitAtLeast reports whether the local `git` is at least the given version.
// Used to skip upstream cherry-pick assertions for protections that older
// Git releases (e.g. 2.11) do not implement, such as the git~1 8.3 short
// name check (CVE-2014-9390 hardening) and the .git::$INDEX_ALLOCATION
// NTFS Alternate Data Stream check (CVE-2019-1351).
func gitAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return false
	}
	var maj, mnr int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d", &maj, &mnr); err != nil {
		return false
	}
	if maj != major {
		return maj > major
	}
	return mnr >= minor
}

func gitCherryPick(t *testing.T, dir, hash string) error {
	t.Helper()
	cmd := exec.Command("git", "cherry-pick", hash)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		abort := exec.Command("git", "cherry-pick", "--abort")
		abort.Dir = dir
		_ = abort.Run()
		return fmt.Errorf("git cherry-pick %s: %s: %w", hash, out, err)
	}
	return nil
}

// TestResetAcceptsLegitPaths drives Reset(HardReset) onto a tree
// containing a variety of legitimate path shapes and asserts each
// one materialises on disk. pathutil.ValidTreePath rejects only
// attacker-shaped names (".git" and equivalents); this test pins
// that the non-attacker tail of the spec — high-codepoint Unicode,
// deeply nested paths, dotfiles, double-dot fragments — passes
// through the strict gate at the materialisation entry point.
func TestResetAcceptsLegitPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping reset path materialisation test in short mode")
	}
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"high-codepoint Unicode dir", "Çircle/file.txt"},
		{"plain nested path", "vendor/lib/main.go"},
		{"plain dotfile", ".gitignore"},
		{"plain gitmodules", ".gitmodules"},
		{"name with double dots not traversal", "a..b/file"},
		{"deep nesting", "a/b/c/d/e/f/file.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			r, err := PlainInit(dir, false)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			w, err := r.Worktree()
			require.NoError(t, err)

			require.NoError(t, util.WriteFile(w.Filesystem(), "README", []byte("init"), 0o644))
			_, err = w.Add("README")
			require.NoError(t, err)

			initHash, err := w.Commit("initial commit\n", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)

			initCommit, err := r.CommitObject(initHash)
			require.NoError(t, err)

			goodCommit := buildCommitWithEntry(t, r.Storer, initCommit, initHash, tc.path, filemode.Regular)

			err = w.Reset(&ResetOptions{Commit: goodCommit.Hash, Mode: HardReset})
			require.NoError(t, err, "Reset should accept legit path %q", tc.path)

			_, err = os.Stat(filepath.Join(dir, filepath.FromSlash(tc.path)))
			require.NoError(t, err, "path %q should exist after Reset", tc.path)
		})
	}
}

// TestAddRejectsDangerousPaths drives Worktree.Add with attacker-shaped
// names that pass the worktreeFilesystem wrapper's tolerant validPath —
// final-position ".git" and HFS+/NTFS .git-disguise variants on
// platforms where the corresponding flag is off — and asserts that the
// strict pathutil.ValidTreePath gate at addOrUpdateFileToIndex refuses
// to record them in the index. Mirrors the chokepoint pattern used by
// Tree.FindEntry on the read side: the wrapper stays tolerant for
// legitimate submodule-cleanup reads, while the index boundary
// guarantees no Add can produce a tree containing such an entry.
//
// Windows reserved device names are not exercised here: they are
// legitimate filenames on non-Windows and upstream Git accepts them, so
// the strict tree-side gate also accepts them. The wrapper rejects them
// at materialisation time when core.protectNTFS is on; that path is
// covered by TestValidPathProtectNTFS.
func TestAddRejectsDangerousPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"final-component .git in subdirectory", "submodule/.git"},
		{"NTFS trailing space on .git", ".git "},
		{"NTFS trailing dot on .git", ".git."},
		{"NTFS alternate data stream", ".git::$INDEX_ALLOCATION"},
		{"NTFS trailing space on git~1", "git~1 "},
		{"NTFS alternate data stream on git~1", "git~1::$DATA"},
		{"HFS+ zero-width character in .git", ".g\u200cit"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			r, err := Init(memory.NewStorage(), WithWorkTree(fs))
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			w, err := r.Worktree()
			require.NoError(t, err)
			// Force the wrapper to its most tolerant configuration so
			// every test case reaches the addOrUpdateFileToIndex gate
			// regardless of host platform. Defaults vary (HFS on Mac,
			// NTFS on Windows) and would short-circuit some shapes at
			// the wrapper layer instead of the boundary under test.
			w.filesystem = newWorktreeFilesystem(fs, false, false)

			require.NoError(t, util.WriteFile(fs, tc.path, []byte("payload"), 0o644))

			_, err = w.Add(tc.path)
			require.Error(t, err, "Add should reject %q", tc.path)
			assert.ErrorIs(t, err, pathutil.ErrInvalidPath)
		})
	}
}

// TestMoveRejectsDangerousDestinations exercises the same boundary as
// TestAddRejectsDangerousPaths from the rename side: Move's destination
// flows through addOrUpdateFileToIndex, so the strict gate must refuse
// renaming a tracked file onto an attacker-shaped name even when the
// wrapper's tolerant validPath would accept the rename itself.
func TestMoveRejectsDangerousDestinations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		to   string
	}{
		{"final-component .git in subdirectory", "submodule/.git"},
		{"NTFS trailing space on .git", ".git "},
		{"NTFS trailing space on git~1", "git~1 "},
		{"HFS+ zero-width character in .git", ".g\u200cit"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			r, err := Init(memory.NewStorage(), WithWorkTree(fs))
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			w, err := r.Worktree()
			require.NoError(t, err)
			w.filesystem = newWorktreeFilesystem(fs, false, false)

			require.NoError(t, util.WriteFile(fs, "src", []byte("payload"), 0o644))
			_, err = w.Add("src")
			require.NoError(t, err)

			_, err = w.Move("src", tc.to)
			require.Error(t, err, "Move should reject destination %q", tc.to)
			assert.ErrorIs(t, err, pathutil.ErrInvalidPath)
		})
	}
}

func TestValidPathProtectNTFS(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), true, false)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{".git . . .", true},
		{".git . . ", true},
		{".git ", true},
		{".git.", true},
		{".git::$INDEX_ALLOCATION", true},
		{"CON", true},
		{"aux.txt", true},
		{"sub/NUL", true},
		{"sub/COM1.txt", true},
		{"CONIN$", true},
		{"readme.md", false},
		{".gitignore", false},
		{"CONNECT", false},
	}

	if runtime.GOOS == "windows" {
		// filepath.VolumeName only parses volume names on Windows.
		tests = append(tests, []struct {
			path    string
			wantErr bool
		}{
			{"\\\\a\\b", true},
			{"C:\\a\\b", true},
		}...)
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(tc.path)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidPathProtectNTFSDisabled(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	paths := []string{
		".git . . .",
		".git ",
		".git.",
		".git::$INDEX_ALLOCATION",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(p)
			assert.NoError(t, err, "NTFS checks should not apply when protectNTFS is false")
		})
	}
}

func TestWorktreeFilesystemRejectsNTFSPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), true, false)

	ntfsPaths := []string{
		".git /config",
		".git./config",
		".git::$INDEX_ALLOCATION/config",
	}

	for _, p := range ntfsPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			_, err := fs.Create(p)
			assert.Error(t, err, "Create should reject NTFS path %q", p)
		})
	}
}

func TestWorktreeFilesystemRejectsNTFSDotGitmodulesSymlink(t *testing.T) {
	t.Parallel()

	tests := []string{
		".gitmodules ",
		".gitmodules.",
		".gitmodules .",
		".gitmodules::$DATA",
		"gitmod~1",
		"GITMOD~4",
		"gi7eba~1",
		"sub/.gitmodules ",
	}

	for _, link := range tests {
		t.Run(link, func(t *testing.T) {
			t.Parallel()
			fs := newWorktreeFilesystem(memfs.New(), true, false)
			err := fs.Symlink("safe-target", link)
			require := assert.New(t)
			require.Error(err, "Symlink should reject %q", link)
			require.ErrorIs(err, ErrGitModulesSymlink, "expected ErrGitModulesSymlink for %q", link)
		})
	}
}

func TestWorktreeFilesystemNTFSDotGitmodulesSymlinkAllowedWhenProtectionOff(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	// Bare .gitmodules is rejected via the case-insensitive match
	// regardless of protectNTFS, but its NTFS variants are allowed
	// when protectNTFS is off.
	err := fs.Symlink("safe-target", ".gitmodules ")
	assert.NoError(t, err, "NTFS variant should be allowed when protectNTFS is off")
}

func TestValidPathProtectHFS(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, true)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{".git", true},
		{".g\u200cit", true},
		{"\u200e.git", true},
		{".Git", true},
		{".GIT", true},
		{".gitignore", false},
		{"readme.md", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(tc.path)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidPathProtectHFSDisabled(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	hfsPaths := []string{
		".g\u200cit",
		"\u200e.git",
		".gi\ufefft",
	}

	for _, p := range hfsPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(p)
			assert.NoError(t, err, "HFS checks should not apply when protectHFS is false")
		})
	}
}

func TestWorktreeFilesystemRejectsHFSPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, true)

	hfsPaths := []string{
		".g\u200cit/config",
		"\u200e.git/config",
	}

	for _, p := range hfsPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, err := fs.Create(p)
			assert.Error(t, err, "Create should reject HFS path %q", p)
		})
	}
}

func TestWorktreeFilesystemRejectsHFSDotGitmodulesSymlink(t *testing.T) {
	t.Parallel()

	tests := []string{
		".g\u200citmodules",
		".gitmod\u200dules",
		"\u200e.gitmodules",
		".gitmodules\ufeff",
		"sub/.g\u200citmodules",
	}

	for _, link := range tests {
		t.Run(link, func(t *testing.T) {
			t.Parallel()
			fs := newWorktreeFilesystem(memfs.New(), false, true)
			err := fs.Symlink("safe-target", link)
			assert.Error(t, err, "Symlink should reject %q", link)
			assert.ErrorIs(t, err, ErrGitModulesSymlink, "expected ErrGitModulesSymlink for %q", link)
		})
	}
}

func TestWorktreeFilesystemHFSDotGitmodulesSymlinkAllowedWhenProtectionOff(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)
	err := fs.Symlink("safe-target", ".g\u200citmodules")
	assert.NoError(t, err, "HFS variant should be allowed when protectHFS is off")
}
