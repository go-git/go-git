package git

import (
	"bytes"
	"fmt"
	"io"
	gofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	billy "github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/internal/test/gitenv"
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
		{"a/.git", true},
		{"a\\.git", true},
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
		{"reject /submodule/.git", "/submodule/.git", true},
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
		skipGit          bool
		acceptOffWindows bool
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
			skipGit: !gitAtLeast(t, 2, 24, 1),
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
			skipGit: !gitAtLeast(t, 2, 24, 1),
		},
		{
			name:             "NTFS reserved device name CON",
			acceptOffWindows: true,
			path:             "CON/file",
			config:           map[string]string{"core.protectNTFS": "true"},
			skipGit:          runtime.GOOS != "windows",
		},
		{
			name:             "NTFS reserved device name NUL",
			acceptOffWindows: true,
			path:             "NUL",
			config:           map[string]string{"core.protectNTFS": "true"},
			skipGit:          runtime.GOOS != "windows",
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
			if tc.acceptOffWindows && runtime.GOOS != "windows" {
				require.NoError(t, goGitErr)
				_, err := w.Filesystem().Lstat(tc.path)
				require.NoError(t, err)
				return
			}
			assert.Error(t, goGitErr, "go-git should reject cherry-pick of %q", tc.path)

			if !tc.skipGit {
				require.NoError(t, w.Reset(&ResetOptions{Commit: initHash, Mode: HardReset}))

				gitErr := gitCherryPick(t, dir, badCommit.Hash.String())
				assert.Error(t, gitErr, "git should reject cherry-pick of %q", tc.path)
			}
		})
	}
}

// TestPathPolicyMatchesGitIndex compares the three gates against
// reference git's own index, row by row, under every combination of
// core.protectNTFS and core.protectHFS. The rule column names the
// expected verdict, so a wrong port shows up as a mismatch against
// git rather than against a predicate this branch also changed.
//
// The divergences are deliberate: go-git refuses the bare 8.3 alias
// git~1 whatever the configuration says, refuses a backslash
// component, and ValidTreePath refuses the ".." disguises git carries
// in a POSIX tree.
//
//nolint:paralleltest // Subtests share one Git index and config.
func TestPathPolicyMatchesGitIndex(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("POSIX Git oracle; native Windows gates have separate tests")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob"), []byte("content"), 0o644))
	blob := gitRun(t, dir, "hash-object", "-w", "blob")

	cases := []struct{ name, rule string }{
		{".git", "literal"},
		{"sub/.git", "literal"},
		{".GIT", "literal"},
		{"sub/.GIT", "literal"},
		{". /.git", "literal"},
		{"::$INDEX_ALLOCATION/.git", "literal"},
		{":a/.git", "literal"},
		{"git~1", "shortname"},
		{"git~1/HEAD", "shortname"},
		{"git~1 ", "ntfs"},
		{"sub/git~1 ", "ntfs"},
		{".git ", "ntfs"},
		{".git::$INDEX_ALLOCATION", "ntfs"},
		{"a\\.git", "backslash"},
		{".git\u200c", "hfs"},
		{"sub/.git\u200c", "hfs"},
		{".gi\u200ct", "hfs"},
		{".g\u200cit", "hfs"},
		{".\u200cgit", "hfs"},
		{"\u200c.git", "hfs"},
		{".. ", "tree-policy"},
		{"..:x", "tree-policy"},
		{".. .", "tree-policy"},
		{"x/.. ", "tree-policy"},
		{"..::$INDEX_ALLOCATION", "tree-policy"},
		{".\u200c./inner.txt", "tree-policy"},
		{"x/.\u200c.", "tree-policy"},
		{"tab\tname", "tree-policy"},
		{"del\x7fname", "tree-policy"},
		{"a\\.", "tree-policy"},
		{"a\\..", "tree-policy"},
		{"trail.", "ordinary"},
		{"trail ", "ordinary"},
		{".../inner.txt", "ordinary"},
		{"....", "ordinary"},
		{"sub /x", "ordinary"},
		{".gitattributes ", "ordinary"},
		{".gitignore ", "ordinary"},
		{".gitmodules ", "ordinary"},
		{".mailmap ", "ordinary"},
		{".GITIGNORE", "ordinary"},
		{"aux.c", "ordinary"},
		{"lib/con.go", "ordinary"},
		{"con c", "ordinary"},
		{"C:foo", "ordinary"},
		{"a:b", "ordinary"},
		{"C:/x", "ordinary"},
		{`\\srv\share\x`, "ordinary"},
		{`\??\C:\x`, "ordinary"},
	}

	for _, ntfs := range []bool{false, true} {
		for _, hfs := range []bool{false, true} {
			gitRun(t, dir, "config", "core.protectNTFS", fmt.Sprint(ntfs))
			gitRun(t, dir, "config", "core.protectHFS", fmt.Sprint(hfs))
			fs := newWorktreeFilesystem(memfs.New(), ntfs, hfs)

			for _, tc := range cases {
				t.Run(fmt.Sprintf("%q/ntfs=%t/hfs=%t", tc.name, ntfs, hfs), func(t *testing.T) {
					gitRun(t, dir, "read-tree", "--empty")
					cmd := gitenv.CommandContext(t.Context(), "git", "-C", dir, "update-index", "--add", "--cacheinfo", "100644", blob, tc.name)
					_, _ = cmd.CombinedOutput()
					indexed := gitRun(t, dir, "ls-files", "-z")

					gitAccepts := tc.rule != "literal" &&
						(!ntfs || (tc.rule != "ntfs" && tc.rule != "shortname" && tc.rule != "backslash")) &&
						(!hfs || tc.rule != "hfs")
					// `core.protectNTFS` has covered `git~1` and the
					// trailing space and period spellings since 2.2.1,
					// but the Alternate Data Stream spelling only since
					// 2.24.1 (7c3745fc6185, CVE-2019-1352)[1]. Earlier
					// releases end a component at a directory separator
					// and never at `:`, so `.git::$INDEX_ALLOCATION`
					// reaches the index with the setting enabled. Ask
					// Git only where it implements the check; the go-git
					// verdicts below are asserted against every release.
					//
					// [1]: https://github.com/git/git/commit/7c3745fc6185495d5765628b4dfe1bd2c25a2981
					ntfsStream := tc.rule == "ntfs" && strings.Contains(tc.name, ":")
					if !ntfsStream || gitAtLeast(t, 2, 24, 1) {
						require.Equal(t, gitAccepts, indexed == tc.name+"\x00", "Git index: %q", indexed)
					}

					worktreeAccepts := gitAccepts && tc.rule != "tree-policy" &&
						tc.rule != "backslash" && tc.rule != "shortname"
					require.Equal(t, worktreeAccepts, fs.validPath(tc.name) == nil)
					require.Equal(t, worktreeAccepts, fs.validWritePath(tc.name) == nil)
					require.Equal(t, tc.rule == "ordinary", pathutil.ValidTreePath(tc.name) == nil)
				})
			}
		}
	}
}

// TestPlainClonePOSIXPathPolicy takes each name through a real git
// commit and then clones it with both implementations. Reference git
// always materialises the file; go-git clones the ordinary names and
// refuses the ones ValidTreePath rejects, which also stops tree
// iteration at the offending entry.
func TestPlainClonePOSIXPathPolicy(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("POSIX filenames and Linux config defaults")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}

	tests := []struct {
		name     string
		rejected bool
	}{
		{"trail.", false},
		{"trail ", false},
		{".../inner.txt", false},
		{". /inner.txt", false},
		{".\u200c/inner.txt", false},
		{"aux.c", false},
		{"lib/con.go", false},
		{"a:b.txt", false},
		{".gitattributes ", false},
		{"gi7eba~1", false},
		{".. ", true},
		{"..:x", true},
		{".. .", true},
		{"x/.. ", true},
		{"..::$INDEX_ALLOCATION", true},
		{".\u200c./inner.txt", true},
		{"x/.\u200c.", true},
		{"tab\tname", true},
		{"del\x7fname", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			source := filepath.Join(root, "source")
			require.NoError(t, os.Mkdir(source, 0o755))
			gitRun(t, source, "init", "-q")

			p := filepath.Join(source, tc.name)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, []byte("payload"), 0o644))
			gitRun(t, source, "add", "--all")
			require.Equal(t, tc.name+"\x00", gitRun(t, source, "ls-files", "-z"))
			gitRun(t, source, "commit", "-qm", "fixture")

			oracle := filepath.Join(root, "git-clone")
			gitRun(t, source, "clone", "-q", source, oracle)
			content, err := os.ReadFile(filepath.Join(oracle, tc.name))
			require.NoError(t, err)
			require.Equal(t, "payload", string(content))

			r, err := PlainOpen(source)
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })
			head, err := r.Head()
			require.NoError(t, err)
			commit, err := r.CommitObject(head.Hash())
			require.NoError(t, err)
			tree, err := commit.Tree()
			require.NoError(t, err)

			iter := tree.Files()
			defer iter.Close()
			file, walkErr := iter.Next()

			clone := filepath.Join(root, "go-clone")
			cloned, cloneErr := PlainClone(clone, &CloneOptions{URL: source})
			// PlainClone returns a non-nil repository alongside some
			// errors, having closed it already; Close is idempotent,
			// so the nil guard is the only one needed.
			if cloned != nil {
				t.Cleanup(func() { _ = cloned.Close() })
			}

			if tc.rejected {
				// This single-offender fixture yields nothing. A mixed
				// tree may yield the files preceding the offender; full
				// iteration stays unavailable either way.
				require.Nil(t, file)
				require.Error(t, walkErr)
				require.NotErrorIs(t, walkErr, io.EOF)
				require.Error(t, cloneErr)
				return
			}

			require.NoError(t, walkErr)
			require.Equal(t, tc.name, file.Name)
			_, err = iter.Next()
			require.ErrorIs(t, err, io.EOF)

			require.NoError(t, cloneErr)
			content, err = os.ReadFile(filepath.Join(clone, tc.name))
			require.NoError(t, err)
			require.Equal(t, "payload", string(content))
		})
	}
}

func TestCherryPickDoesNotWriteThroughLeadingSymlink(t *testing.T) {
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

	configPath := filepath.Join(dir, ".git", "config")
	originalConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)

	attack := buildCommitWithEntry(t, r.Storer, initCommit, initHash, "s/config", filemode.Regular)

	err = w.Filesystem().Symlink(".git", "s")
	if err != nil && isSymlinkWindowsNonAdmin(err) {
		t.Skipf("symlink creation requires elevated privileges on this platform: %v", err)
	}
	require.NoError(t, err)

	err = w.CherryPick(
		&CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true},
		TheirsMergeStrategy, attack,
	)
	require.NoError(t, err)

	gotConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, originalConfig, gotConfig, "CherryPick must not write s/config through symlink s -> .git")

	got, err := os.ReadFile(filepath.Join(dir, "s", "config"))
	require.NoError(t, err)
	require.Equal(t, "exploit", string(got))
}

func TestCherryPickDoesNotWriteThroughFinalSymlink(t *testing.T) {
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

	configPath := filepath.Join(dir, ".git", "config")
	originalConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)

	attack := buildCommitWithEntry(t, r.Storer, initCommit, initHash, "s", filemode.Regular)

	err = w.Filesystem().Symlink(".git/config", "s")
	if err != nil && isSymlinkWindowsNonAdmin(err) {
		t.Skipf("symlink creation requires elevated privileges on this platform: %v", err)
	}
	require.NoError(t, err)

	err = w.CherryPick(
		&CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true},
		TheirsMergeStrategy, attack,
	)

	gotConfig, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	require.Equal(t, originalConfig, gotConfig, "CherryPick must not write s through symlink s -> .git/config")

	if err == nil {
		fi, statErr := os.Lstat(filepath.Join(dir, "s"))
		require.NoError(t, statErr)
		require.Zero(t, fi.Mode()&os.ModeSymlink, "successful CherryPick must replace the symlink with a regular file")

		got, readErr := os.ReadFile(filepath.Join(dir, "s"))
		require.NoError(t, readErr)
		require.Equal(t, "exploit", string(got))
	}
}

func TestCherryPickModifyTrackedSymlinkToRegularFileDoesNotWriteThroughSymlink(t *testing.T) {
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

	_, err = w.Commit("initial commit\n", &CommitOptions{Author: defaultSignature()})
	require.NoError(t, err)

	err = w.Filesystem().Symlink(".git/config", "s")
	if err != nil && isSymlinkWindowsNonAdmin(err) {
		t.Skipf("symlink creation requires elevated privileges on this platform: %v", err)
	}
	require.NoError(t, err)

	_, err = w.Add("s")
	require.NoError(t, err)
	linkHash, err := w.Commit("add symlink\n", &CommitOptions{Author: defaultSignature()})
	require.NoError(t, err)

	linkCommit, err := r.CommitObject(linkHash)
	require.NoError(t, err)

	configPath := filepath.Join(dir, ".git", "config")
	originalConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)

	attack := buildCommitReplacingRootEntry(t, r.Storer, linkCommit, linkHash, object.TreeEntry{
		Name: "s",
		Mode: filemode.Regular,
		Hash: writeBlob(t, r.Storer, []byte("exploit")),
	})

	err = w.CherryPick(
		&CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true},
		TheirsMergeStrategy, attack,
	)

	gotConfig, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	require.Equal(t, originalConfig, gotConfig, "CherryPick must not write tracked symlink s through to .git/config")
	require.NoError(t, err)

	fi, err := os.Lstat(filepath.Join(dir, "s"))
	require.NoError(t, err)
	require.Zero(t, fi.Mode()&os.ModeSymlink, "CherryPick should replace the tracked symlink with a regular file")

	got, err := os.ReadFile(filepath.Join(dir, "s"))
	require.NoError(t, err)
	require.Equal(t, "exploit", string(got))
}

func buildCommitWithEntry(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, filePath string, leafMode filemode.FileMode) *object.Commit {
	t.Helper()

	leafHash := writeBlob(t, s, []byte("exploit"))

	// Build nested tree structure from leaf to root.
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

func buildCommitReplacingRootEntry(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, replacement object.TreeEntry) *object.Commit {
	t.Helper()

	parentTree, err := parent.Tree()
	require.NoError(t, err)

	entries := make([]object.TreeEntry, 0, len(parentTree.Entries)+1)
	for _, e := range parentTree.Entries {
		if e.Name == replacement.Name {
			continue
		}
		entries = append(entries, e)
	}
	entries = append(entries, replacement)
	sort.Sort(object.TreeEntrySorter(entries))
	rootHash := storeRawTree(t, s, entries)

	commit := &object.Commit{
		Author:       *defaultSignature(),
		Committer:    *defaultSignature(),
		Message:      "replace " + replacement.Name + "\n",
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

// buildCommitWithEntries builds a commit on top of parent whose tree is
// the parent tree plus extra. Unlike buildCommitWithEntry it takes
// fully-formed entries, so callers can plant symlinks and subtrees.
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
	cmd := gitenv.Command("git", "config", key, value)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git config %s %s: %s", key, value, out)
}

// gitAtLeast reports whether the local `git` is at least the given version.
// Used to skip upstream assertions for protections that older Git releases
// do not apply. Both protections the callers gate on arrived in 2.24.1:
// `core.protectNTFS` has covered the `git~1` short name since 2.2.1 but
// only defaults to enabled since 9102f958ee5 (CVE-2019-1353)[1], and
// is_ntfs_dotgit reads the `.git::$INDEX_ALLOCATION` Alternate Data Stream
// spelling only since 7c3745fc6185 (CVE-2019-1352)[2].
//
// [1]: https://github.com/git/git/commit/9102f958ee5
// [2]: https://github.com/git/git/commit/7c3745fc6185
func gitAtLeast(t *testing.T, major, minor, patch int) bool {
	t.Helper()
	out, err := gitenv.Command("git", "--version").Output()
	if err != nil {
		return false
	}
	// Release builds carry a patch component; builds from a development
	// branch append further fields, which Sscanf leaves unread.
	var maj, mnr, pch int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d.%d", &maj, &mnr, &pch); err != nil {
		if _, err := fmt.Sscanf(string(out), "git version %d.%d", &maj, &mnr); err != nil {
			return false
		}
	}
	switch {
	case maj != major:
		return maj > major
	case mnr != minor:
		return mnr > minor
	default:
		return pch >= patch
	}
}

func gitCherryPick(t *testing.T, dir, hash string) error {
	t.Helper()
	cmd := gitenv.Command("git", "cherry-pick", hash)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		abort := gitenv.Command("git", "cherry-pick", "--abort")
		abort.Dir = dir
		_ = abort.Run()
		return fmt.Errorf("git cherry-pick %s: %s: %w", hash, out, err)
	}
	return nil
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()

	overrides := []string{
		"-c", "protocol.file.allow=always",
		"-c", "submodule.recurse=false",
		"-C", dir,
	}
	out, err := gitenv.CommandContext(t.Context(), "git", append(overrides, args...)...).CombinedOutput()
	require.NoError(t, err, "git %q: %s", args, out)
	return strings.TrimSpace(string(out))
}

// recordingFS records every path handed to a mutating operation, so a
// test can assert on which strings actually reach the filesystem
// after the wrapper's validators have had their say. Asserting on the
// returned error is not enough: a delete that succeeds and a delete
// that is refused can both leave Reset returning nil.
type recordingFS struct {
	billy.Filesystem
	calls []string
}

func (r *recordingFS) record(op, p string) { r.calls = append(r.calls, op+" "+p) }

func (r *recordingFS) Remove(p string) error {
	r.record("Remove", p)
	return r.Filesystem.Remove(p)
}

func (r *recordingFS) OpenFile(p string, flag int, perm gofs.FileMode) (billy.File, error) {
	r.record("OpenFile", p)
	return r.Filesystem.OpenFile(p, flag, perm)
}

func (r *recordingFS) MkdirAll(p string, perm gofs.FileMode) error {
	r.record("MkdirAll", p)
	return r.Filesystem.MkdirAll(p, perm)
}

func (r *recordingFS) sawPath(name string) bool {
	for _, c := range r.calls {
		if strings.HasSuffix(c, " "+name) {
			return true
		}
	}
	return false
}

// storeCommit writes commit to s and returns its hash.
func storeCommit(t *testing.T, s storer.Storer, commit *object.Commit) plumbing.Hash {
	t.Helper()

	obj := s.NewEncodedObject()
	require.NoError(t, commit.Encode(obj))
	hash, err := s.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

// TestResetHardRefusesTreeDerivedDotDotDisguise closes the delete
// half of checkout and reset. resetWorktreeToTree's first pass takes
// ch.From.String() straight from diffTrees into rmEntryAndDirsIfEmpty,
// and diffTrees' treeNoder sets TreeWalker.skipPathValidation, so the
// name never meets pathutil.ValidTreePath. The wrapper's validPath is
// the only gate, and it must hold with both protections off as well
// as on.
//
// Reachability does not require the hostile tree ever to have been
// materialised: Reset sets HEAD before resetIndex, so an aborted
// checkout leaves HEAD on the hostile commit and the next
// reset --hard issues the delete. The test models exactly that by
// pointing HEAD at the hostile commit directly.
func TestResetHardRefusesTreeDerivedDotDotDisguise(t *testing.T) {
	t.Parallel()

	const hostile = ".. "

	for _, tc := range []struct {
		name        string
		protectNTFS config.OptBool
		protectHFS  config.OptBool
	}{
		{name: "repository defaults"},
		{
			name:        "protections off",
			protectNTFS: config.NewOptBool(false),
			protectHFS:  config.NewOptBool(false),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := memory.NewStorage()
			rec := &recordingFS{Filesystem: memfs.New()}

			r, err := Init(s, WithWorkTree(rec))
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })

			if tc.protectNTFS.IsSet() || tc.protectHFS.IsSet() {
				cfg, err := r.Config()
				require.NoError(t, err)
				cfg.Core.ProtectNTFS = tc.protectNTFS
				cfg.Core.ProtectHFS = tc.protectHFS
				require.NoError(t, r.SetConfig(cfg))
			}

			blob := writeBlob(t, s, []byte("payload\n"))
			hostileTree := storeRawTree(t, s, []object.TreeEntry{
				{Name: hostile, Mode: filemode.Regular, Hash: blob},
			})
			benignTree := storeRawTree(t, s, nil)

			sig := defaultSignature()
			hostileCommit := storeCommit(t, s, &object.Commit{
				Author:    *sig,
				Committer: *sig,
				Message:   "hostile\n",
				TreeHash:  hostileTree,
			})
			benignCommit := storeCommit(t, s, &object.Commit{
				Author:       *sig,
				Committer:    *sig,
				Message:      "benign\n",
				TreeHash:     benignTree,
				ParentHashes: []plumbing.Hash{hostileCommit},
			})

			// The hostile name really is in the stored tree, and the
			// tree-path validator really does refuse it.
			tr, err := object.GetTree(s, hostileTree)
			require.NoError(t, err)
			require.Len(t, tr.Entries, 1)
			require.Equal(t, hostile, tr.Entries[0].Name)
			_, err = tr.FindEntry(hostile)
			require.Error(t, err, "FindEntry must refuse the disguise")

			head, err := r.Reference(plumbing.HEAD, false)
			require.NoError(t, err)
			require.NoError(t, s.SetReference(
				plumbing.NewHashReference(head.Target(), hostileCommit),
			))

			w, err := r.Worktree()
			require.NoError(t, err)

			err = w.Reset(&ResetOptions{Mode: HardReset, Commit: benignCommit})
			t.Logf("Reset returned: %v", err)
			t.Logf("filesystem calls: %q", rec.calls)

			assert.False(t, rec.sawPath(hostile),
				"the disguise %q must never reach the filesystem; calls=%q",
				hostile, rec.calls)
		})
	}
}

// TestResetHardHonoursTheRemovedEntryMode drives the two mode-dependent
// removals through a reset --hard, where resetWorktreeToTree reads the mode
// from the tree the diff is taken from.
//
// Both shapes are ones a user can leave in a worktree: a submodule directory
// replaced by a file, and a tracked file replaced by a directory. Upstream
// Git's remove_or_warn refuses each of them and warns, so the reset must
// leave both in place.
func TestResetHardHonoursTheRemovedEntryMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mode filemode.FileMode
		// directory is the shape standing in the worktree at the entry's
		// path, in place of what mode describes.
		directory bool
	}{
		{name: "file at a gitlink", mode: filemode.Submodule},
		{name: "directory at a regular entry", mode: filemode.Regular, directory: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const name = "entry"

			s := memory.NewStorage()
			wt := memfs.New()

			r, err := Init(s, WithWorkTree(wt))
			require.NoError(t, err)
			t.Cleanup(func() { _ = r.Close() })

			// A gitlink names a commit in another repository, so its
			// hash need not resolve here. A blob's must.
			target := plumbing.NewHash("0123456789012345678901234567890123456789")
			if tc.mode != filemode.Submodule {
				target = writeBlob(t, s, []byte("tracked\n"))
			}

			fromTree := storeRawTree(t, s, []object.TreeEntry{
				{Name: name, Mode: tc.mode, Hash: target},
			})
			toTree := storeRawTree(t, s, nil)

			sig := defaultSignature()
			from := storeCommit(t, s, &object.Commit{
				Author:    *sig,
				Committer: *sig,
				Message:   "carries the entry\n",
				TreeHash:  fromTree,
			})
			to := storeCommit(t, s, &object.Commit{
				Author:       *sig,
				Committer:    *sig,
				Message:      "drops the entry\n",
				TreeHash:     toTree,
				ParentHashes: []plumbing.Hash{from},
			})

			head, err := r.Reference(plumbing.HEAD, false)
			require.NoError(t, err)
			require.NoError(t, s.SetReference(
				plumbing.NewHashReference(head.Target(), from),
			))

			if tc.directory {
				require.NoError(t, wt.MkdirAll(name, 0o755))
			} else {
				require.NoError(t, util.WriteFile(wt, name, []byte("user data\n"), 0o644))
			}

			w, err := r.Worktree()
			require.NoError(t, err)
			require.NoError(t, w.Reset(&ResetOptions{Mode: HardReset, Commit: to}))

			fi, err := wt.Lstat(name)
			require.NoError(t, err, "the reset must leave %q in place", name)
			require.Equal(t, tc.directory, fi.IsDir(), "the reset must not change the shape at %q", name)
		})
	}
}

// TestResetRejectsDotGitPositionShift walks a .git alias through the
// positions a hostile tree can hide it in — behind a component that
// NTFS or HFS+ folds away, behind an Alternate Data Stream name, and
// at the root — for a regular entry and for a gitlink. The gate has
// to hold with either protection off, because ValidTreePath refuses
// the alias unconditionally and the wrapper is the only thing the
// tree-derived delete passes through.
func TestResetRejectsDotGitPositionShift(t *testing.T) {
	t.Parallel()

	names := []string{
		"::$INDEX_ALLOCATION/.git",
		":a/.git",
		".\u200c/.git",
		". /.git",
		".../.git",
		" /.git",
		"sub/.GIT",
		".git",
	}

	for _, name := range names {
		for _, mode := range []filemode.FileMode{filemode.Regular, filemode.Submodule} {
			for _, ntfs := range []bool{false, true} {
				for _, hfs := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/%o/ntfs=%t/hfs=%t", name, mode, ntfs, hfs), func(t *testing.T) {
						t.Parallel()

						s := memory.NewStorage()
						rec := &recordingFS{Filesystem: memfs.New()}

						r, err := Init(s, WithWorkTree(rec))
						require.NoError(t, err)
						t.Cleanup(func() { _ = r.Close() })

						cfg, err := r.Config()
						require.NoError(t, err)
						cfg.Core.ProtectNTFS = config.NewOptBool(ntfs)
						cfg.Core.ProtectHFS = config.NewOptBool(hfs)
						require.NoError(t, r.SetConfig(cfg))

						blob := writeBlob(t, s, []byte("payload"))
						hostileTree := storeRawTree(t, s, []object.TreeEntry{
							{Name: name, Mode: mode, Hash: blob},
						})
						benignTree := storeRawTree(t, s, nil)

						sig := defaultSignature()
						hostileCommit := storeCommit(t, s, &object.Commit{
							Author:    *sig,
							Committer: *sig,
							Message:   "hostile\n",
							TreeHash:  hostileTree,
						})
						benignCommit := storeCommit(t, s, &object.Commit{
							Author:       *sig,
							Committer:    *sig,
							Message:      "benign\n",
							TreeHash:     benignTree,
							ParentHashes: []plumbing.Hash{hostileCommit},
						})

						head, err := r.Reference(plumbing.HEAD, false)
						require.NoError(t, err)
						require.NoError(t, s.SetReference(
							plumbing.NewHashReference(head.Target(), hostileCommit),
						))

						w, err := r.Worktree()
						require.NoError(t, err)

						rec.calls = nil
						err = w.Reset(&ResetOptions{Mode: HardReset, Commit: benignCommit})
						require.Error(t, err, "the alias must be refused; calls=%q", rec.calls)
						assert.False(t, rec.sawPath(name),
							"the alias %q must never reach the filesystem; calls=%q",
							name, rec.calls)
					})
				}
			}
		}
	}
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

// TestCheckoutMaterialisesPOSIXTrailingNames drives a checkout of the
// names the Win32 component rules refuse and the host gate now
// allows. Off Windows every one has to reach the filesystem with its
// contents intact: they are ordinary POSIX filenames that C Git
// checks out, and refusing them made a repository containing aux.c
// or ".gitattributes " unusable.
func TestCheckoutMaterialisesPOSIXTrailingNames(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX contract")
	}

	paths := []string{
		"trail.", "trail ", ".../inner.txt", ". /inner.txt",
		".\u200c/inner.txt", "aux.c", "a:b.txt", ".gitattributes ",
		"gi7eba~1",
	}

	fs := memfs.New()
	s := memory.NewStorage()
	r, err := Init(s, WithWorkTree(fs))
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	blob := writeBlob(t, s, []byte("payload"))

	var entries []object.TreeEntry
	for _, p := range paths {
		parent, child, nested := strings.Cut(p, "/")
		if !nested {
			entries = append(entries, object.TreeEntry{Name: p, Mode: filemode.Regular, Hash: blob})
			continue
		}
		subtree := storeRawTree(t, s, []object.TreeEntry{
			{Name: child, Mode: filemode.Regular, Hash: blob},
		})
		entries = append(entries, object.TreeEntry{Name: parent, Mode: filemode.Dir, Hash: subtree})
	}
	sort.Sort(object.TreeEntrySorter(entries))

	sig := defaultSignature()
	commit := storeCommit(t, s, &object.Commit{
		Author:    *sig,
		Committer: *sig,
		Message:   "fixture\n",
		TreeHash:  storeRawTree(t, s, entries),
	})

	w, err := r.Worktree()
	require.NoError(t, err)
	require.NoError(t, w.Checkout(&CheckoutOptions{Hash: commit, Force: true}))

	for _, p := range paths {
		body, err := util.ReadFile(fs, p)
		require.NoError(t, err, "path %q should exist after Checkout", p)
		require.Equal(t, "payload", string(body), "path %q", p)
	}
}

// TestForceCheckoutRejectsDangerousPaths pins that a Force checkout
// (Checkout{Force:true}) and its underlying HardReset refuse to
// materialise attacker-shaped tree entries into the worktree.
//
// TestCherryPickPathValidationMatchesGit covers the merge/non-force
// flow; this test closes the gap for the Force path. Force resolves to
// a HardReset, which drives resetIndex and resetWorktreeToTree. Both
// funnel every tree-derived path through Tree.FindEntry, so the strict
// pathutil.ValidTreePath gate rejects a dangerous entry before anything
// is written to disk. Each case asserts the operation errors and that
// the exploit blob never lands anywhere under the repository directory.
func TestForceCheckoutRejectsDangerousPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping force checkout path validation test in short mode")
	}
	t.Parallel()

	// The blob content buildCommitWithEntry writes into the crafted
	// tree entry; used to detect any partial materialisation on disk.
	const exploit = "exploit"

	paths := []struct {
		name string
		path string
	}{
		{".git at root", ".git/config"},
		{".git in subdirectory", "subdir/.git/config"},
		{"final-component .git", "submodule/.git"},
		{"git~1 8.3 short name", "git~1/config"},
		{"dot-dot traversal", "a/../../etc/passwd"},
		{"single dot component", "a/./b"},
		{"NTFS trailing space on .git", ".git /config"},
		{"NTFS trailing dot on .git", ".git./config"},
		{"NTFS alternate data stream", ".git::$INDEX_ALLOCATION/config"},
		{"HFS+ zero-width in .git", ".g\u200cit/config"},
		{"control character", "a\x01b/config"},
	}

	modes := []struct {
		name string
		run  func(w *Worktree, h plumbing.Hash) error
	}{
		{"checkout-force", func(w *Worktree, h plumbing.Hash) error {
			return w.Checkout(&CheckoutOptions{Hash: h, Force: true})
		}},
		{"reset-hard", func(w *Worktree, h plumbing.Hash) error {
			return w.Reset(&ResetOptions{Commit: h, Mode: HardReset})
		}},
	}

	for _, tc := range paths {
		for _, m := range modes {
			t.Run(tc.name+"/"+m.name, func(t *testing.T) {
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

				bad := buildCommitWithEntry(t, r.Storer, initCommit, initHash, tc.path, filemode.Regular)

				err = m.run(w, bad.Hash)
				require.Error(t, err, "%s should reject dangerous path %q", m.name, tc.path)

				assertNotMaterialised(t, dir, exploit)
			})
		}
	}
}

// assertNotMaterialised fails if any regular file under root holds the
// given content, catching a dangerous tree entry that was written to
// disk before validation aborted the operation.
func assertNotMaterialised(t *testing.T, root, content string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if string(b) == content {
			t.Errorf("dangerous entry materialised at %s", path)
		}
		return nil
	})
	require.NoError(t, err)
}

// TestForceCheckoutReplacesLeadingSymlink covers the symlink-mask bypass
// (CVE-2021-21300 class). A symlink "s" pointing at a dangerous directory
// is present in the worktree, planted by an attacker or left by an earlier
// checkout step, and the commit being force-checked-out writes a file at
// "s/<leaf>". That path string is innocent (no ".git" or ".." component),
// so string validation alone lets it through. Following the symlink would
// let the write escape the worktree.
//
// Matching upstream Git's create_directories, a force checkout must
// remove the blocking symlink and materialise a real directory, writing
// the file safely inside the worktree. Each case asserts the checkout
// succeeds, the dangerous target is untouched, and the file lands in the
// worktree instead.
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

			linkTarget, entryPath, escapePath := tc.setup(t, dir)

			attack := buildCommitWithEntry(t, r.Storer, initCommit, initHash, entryPath, filemode.Regular)

			// Plant the masking symlink on disk via the bare filesystem.
			// This models the pre-existing symlink an attacker relies on.
			require.NoError(t, w.Filesystem().Symlink(linkTarget, "s"))

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
// on Windows when core.protectNTFS is on; that path is
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

// TestValidPathRejectsDotGitEveryPosition walks a .git alias through
// every position a path can put it in. `.git` and the bare 8.3 short
// name `git~1` are refused whatever the configuration says, because
// ValidTreePath refuses them; core.protectNTFS and core.protectHFS
// only add the spellings those filesystems fold back to one of the
// two.
func TestValidPathRejectsDotGitEveryPosition(t *testing.T) {
	t.Parallel()

	groups := []struct {
		name  string
		paths []string
	}{
		{"always", []string{
			". /.git", ".\u200c/.git", ".../.git", "..../.git", " /.git",
			"::$INDEX_ALLOCATION/.git", ":a/.git", "a/. /.git",
			"sub/.git", "sub/.GIT", ".GIT", "a\\.git", "a/.git/config",
			"git~1", "git~1/HEAD", "sub/git~1", "GIT~1",
		}},
		{"ntfs", []string{
			"sub/GIT~1 ", ".git ", "git~1 ", ".git::$INDEX_ALLOCATION",
		}},
		{"hfs", []string{
			"sub/.git\u200c", "sub/.gi\u200ct", "sub/.g\u200cit",
			"sub/.\u200cgit", "sub/\u200c.git",
		}},
	}

	for _, group := range groups {
		for _, ntfs := range []bool{false, true} {
			for _, hfs := range []bool{false, true} {
				for _, p := range group.paths {
					t.Run(fmt.Sprintf("%s/%q/ntfs=%t/hfs=%t", group.name, p, ntfs, hfs), func(t *testing.T) {
						t.Parallel()

						fs := newWorktreeFilesystem(memfs.New(), ntfs, hfs)
						rejected := group.name == "always" ||
							group.name == "ntfs" && ntfs ||
							group.name == "hfs" && hfs

						for _, check := range []func(...string) error{fs.validPath, fs.validWritePath} {
							err := check(p)
							if rejected {
								require.Error(t, err)
							} else {
								require.NoError(t, err)
							}
						}
					})
				}
			}
		}
	}
}

// TestWorktreeOperationsSurviveDotGitDisguises pins what the
// every-position refusal costs the porcelain: a name the gate refuses
// is invisible to Status and untouched by Clean, where git would list
// and remove it. Neither operation may fail on account of it, and an
// unrelated file in the same worktree stays reachable.
func TestWorktreeOperationsSurviveDotGitDisguises(t *testing.T) {
	t.Parallel()

	names := []string{
		".git", "git~1", ".GIT", "sub/.GIT",
		"sub/git~1", "sub/.git", ".git\u200c", "sub/.git\u200c",
	}

	for _, name := range names {
		for _, directory := range []bool{false, true} {
			for _, op := range []string{"Clean", "Status", "AddUnrelated", "AddGlob", "AddAll", "AddName"} {
				t.Run(fmt.Sprintf("%q/directory=%t/%s", name, directory, op), func(t *testing.T) {
					t.Parallel()

					fs := memfs.New()
					r, err := Init(memory.NewStorage(), WithWorkTree(fs))
					require.NoError(t, err)
					t.Cleanup(func() { _ = r.Close() })

					cfg, err := r.Config()
					require.NoError(t, err)
					cfg.Core.ProtectNTFS = config.OptBoolTrue
					cfg.Core.ProtectHFS = config.OptBoolTrue
					require.NoError(t, r.SetConfig(cfg))

					w, err := r.Worktree()
					require.NoError(t, err)

					p := name
					if directory {
						p += "/inner.txt"
					}
					require.NoError(t, util.WriteFile(fs, p, []byte("preserved"), 0o644))
					require.NoError(t, util.WriteFile(fs, "unrelated.txt", []byte("safe"), 0o644))

					switch op {
					case "Clean":
						require.NoError(t, w.Clean(&CleanOptions{Dir: true}))
						body, err := util.ReadFile(fs, p)
						require.NoError(t, err)
						require.Equal(t, "preserved", string(body))
					case "Status":
						status, err := w.Status()
						require.NoError(t, err)
						require.Equal(t, Status{
							"unrelated.txt": &FileStatus{Staging: Untracked, Worktree: Untracked},
						}, status)
					case "AddUnrelated":
						_, err = w.Add("unrelated.txt")
						require.NoError(t, err)
					case "AddGlob":
						require.NoError(t, w.AddGlob("."))
					case "AddAll":
						require.NoError(t, w.AddWithOptions(&AddOptions{All: true}))
					case "AddName":
						_, err = w.Add(name)
						require.ErrorIs(t, err, pathutil.ErrInvalidPath)
					}
				})
			}
		}
	}
}

// TestValidPathAcceptsPOSIXNamesOffWindows pins the other half of the
// host gate. Every name here is an ordinary filename that C Git
// carries, so validPath has to accept it off Windows however
// core.protectNTFS is set. On a Win32 host the win32 rows become
// refusals under core.protectNTFS, and the volume rows are refused by
// the host alone, whatever the setting.
func TestValidPathAcceptsPOSIXNamesOffWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		// win32 marks a name only the Win32 component rules refuse;
		// volume one carrying a Windows volume prefix.
		win32, volume bool
	}{
		{"trail.", true, false},
		{"trail ", true, false},
		{".../inner.txt", true, false},
		{"sub /x", true, false},
		{". /inner.txt", true, false},
		{".\u200c/inner.txt", false, false},
		{".gitattributes ", true, false},
		{".gitignore ", true, false},
		{".mailmap ", true, false},
		{"gi7eba~1", false, false},
		{"aux.c", true, false},
		{"lib/con.go", true, false},
		{"C:foo", false, true},
		{"a:b", false, true},
		{"C:/x", false, true},
		{`\\srv\share\x`, false, true},
		{`\??\C:\x`, false, true},
		{".gitattributes /inner.txt", true, false},
	}

	for _, tc := range tests {
		for _, ntfs := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/ntfs=%t", tc.path, ntfs), func(t *testing.T) {
				t.Parallel()

				fs := newWorktreeFilesystem(memfs.New(), ntfs, false)
				err := fs.validPath(tc.path)
				if runtime.GOOS == "windows" && (tc.volume || ntfs && tc.win32) {
					require.Error(t, err)
					if !tc.volume {
						require.Contains(t, err.Error(), "core.protectNTFS")
					}
					return
				}
				require.NoError(t, err)
			})
		}
	}
}

// TestWorktreeAPISurvivesUntrackedEdgeNames is the counterpart to
// TestWorktreeOperationsSurviveDotGitDisguises: these names are not
// disguises, so off Windows the porcelain has to treat them as
// ordinary untracked files — Status lists them, Clean removes them,
// and Add takes them by name.
func TestWorktreeAPISurvivesUntrackedEdgeNames(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX filenames")
	}

	names := []string{
		"build ", ". ", ".\u200c", ".gitattributes ", ".gitignore ",
		".mailmap ", "gi7eba~1", ".GITIGNORE ", "a:b", "aux.c",
	}

	for _, name := range names {
		for _, directory := range []bool{false, true} {
			for _, op := range []string{"Clean", "Status", "AddUnrelated", "AddGlob", "AddAll", "AddName"} {
				t.Run(fmt.Sprintf("%q/directory=%t/%s", name, directory, op), func(t *testing.T) {
					t.Parallel()

					fs := memfs.New()
					r, err := Init(memory.NewStorage(), WithWorkTree(fs))
					require.NoError(t, err)
					t.Cleanup(func() { _ = r.Close() })

					w, err := r.Worktree()
					require.NoError(t, err)

					p := name
					if directory {
						p += "/inner.txt"
					}
					require.NoError(t, util.WriteFile(fs, p, []byte("content"), 0o644))
					require.NoError(t, util.WriteFile(fs, "unrelated.txt", []byte("safe"), 0o644))

					switch op {
					case "Clean":
						require.NoError(t, w.Clean(&CleanOptions{Dir: true}))
						_, err = fs.Lstat(name)
						require.ErrorIs(t, err, os.ErrNotExist)
					case "Status":
						status, err := w.Status()
						require.NoError(t, err)
						require.Contains(t, status, p)
					case "AddUnrelated":
						_, err = w.Add("unrelated.txt")
						require.NoError(t, err)
					case "AddGlob":
						require.NoError(t, w.AddGlob("."))
					case "AddAll":
						require.NoError(t, w.AddWithOptions(&AddOptions{All: true}))
					case "AddName":
						_, err = w.Add(name)
						require.NoError(t, err)
					}
				})
			}
		}
	}
}

// TestValidPathProtectNTFS runs every row against both hosts. Setting
// worktreeFilesystem.win32 by hand rather than reading runtime.GOOS is
// what makes the Win32 half reachable from a POSIX test run: with a
// runtime.GOOS test in validPath, deleting the Windows policy outright
// still passes the suite everywhere but Windows.
func TestValidPathProtectNTFS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		// win32 is the verdict on a Win32 host, posix on every other.
		win32, posix bool
	}{
		{".git . . .", true, true},
		{".git . . ", true, true},
		{".git ", true, true},
		{".git.", true, true},
		{".git::$INDEX_ALLOCATION", true, true},
		{"CON", true, false},
		{"aux.txt", true, false},
		{"sub/NUL", true, false},
		{"sub/COM1.txt", true, false},
		{"CONIN$", true, false},
		{"foo ", true, false},
		{"foo.", true, false},
		{"sub /x", true, false},
		{".gitattributes ", true, false},
		{".gitignore ", true, false},
		{"...", true, false},
		{"....", true, false},
		// Volume prefixes are a Win32 rule too, and independent of
		// core.protectNTFS.
		{"\\\\a\\b", true, false},
		{"C:\\a\\b", true, false},
		{"a..b", false, false},
		{"foo", false, false},
		{"readme.md", false, false},
		{".gitignore", false, false},
		{"CONNECT", false, false},
	}

	for _, win32 := range []bool{false, true} {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%s/win32=%t", tc.path, win32), func(t *testing.T) {
				t.Parallel()
				fs := newWorktreeFilesystem(memfs.New(), true, false)
				fs.win32 = win32
				wantErr := tc.posix
				if win32 {
					wantErr = tc.win32
				}
				err := fs.validPath(tc.path)
				if wantErr {
					assert.Error(t, err)
					assert.ErrorIs(t, err, pathutil.ErrInvalidPath)
				} else {
					assert.NoError(t, err)
				}
			})
		}
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
		"foo ",
		"foo.",
		"sub /x",
		".gitattributes ",
		"...",
		"....",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(p)
			assert.NoError(t, err, "NTFS checks should not apply when protectNTFS is false")
		})
	}
}

func TestWorktreeFilesystemWin32InvalidCharacters(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"foo:bar", "foo::$DATA", "foo<bar", "foo>bar", "foo\"bar",
		"foo|bar", "foo?bar", "foo*bar", "LPT0", "con .txt",
	} {
		for _, prefix := range []string{"", "sub/", `sub\`} {
			for _, win32 := range []bool{false, true} {
				for _, ntfs := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/win32=%t/ntfs=%t", prefix+name, win32, ntfs), func(t *testing.T) {
						t.Parallel()
						rec := &recordingFS{Filesystem: memfs.New()}
						fs := newWorktreeFilesystem(rec, ntfs, false)
						fs.win32 = win32
						file, err := fs.OpenFile(prefix+name, os.O_CREATE|os.O_WRONLY, 0o644)
						if file != nil {
							require.NoError(t, file.Close())
						}
						if win32 && ntfs {
							require.ErrorIs(t, err, pathutil.ErrInvalidPath)
							require.Empty(t, rec.calls)
						} else {
							require.NoError(t, err)
							require.Equal(t, []string{"OpenFile " + prefix + name}, rec.calls)
						}
					})
				}
			}
		}
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

// TestValidPathRejectsDotDotDisguisesWithProtectionOff pins the
// disguise check as independent of core.protectNTFS and
// core.protectHFS. It has to be: resetWorktreeToTree's first pass
// takes its delete paths from diffTrees, whose treeNoder sets
// TreeWalker.skipPathValidation, so those names never meet
// pathutil.ValidTreePath and this wrapper is their only gate. The
// index decoder does not validate entry names either. Turning
// core.protectNTFS off is a statement about NTFS canonicalisation,
// not consent to a parent hop.
func TestValidPathRejectsDotDotDisguisesWithProtectionOff(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	paths := []string{
		"..",
		"../x",
		".. ",
		".. /x",
		"..  /x",
		".. ./x",
		"..:$DATA/x",
		"..:x/x",
		"..::$INDEX_ALLOCATION/x",
		".\u200c./x",
		"\u200c../x",
		"..\u200c/x",
		"a/.. /b",
		"a\\.. \\b",
		"a/.\u200c./b",
		".",
		"a/./b",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, fs.validPath(p),
				"validPath(%q) must be refused with both protections off", p)
		})
	}
}

// TestValidPathAllowsDotsOnlyWithProtectionOff checks that the Win32
// trailing rule is disabled with core.protectNTFS off. It applies only
// on Windows and is separate from the always-on dot/parent check.
func TestValidPathAllowsDotsOnlyWithProtectionOff(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	for _, p := range []string{"...", "....", "a/.../b", "x..", ".. x", ". "} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, fs.validPath(p),
				"validPath(%q) must be allowed with core.protectNTFS off", p)
		})
	}
}
