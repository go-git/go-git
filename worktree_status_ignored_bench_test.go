package git

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/object"
)

// setupIgnoredDirRepo builds a repo with `tracked` source files committed, a
// `.gitignore` excluding `ignoredDir`, and `untracked` files dropped into
// `ignoredDir`. The ignored directory is a stand-in for `node_modules`,
// `vendor`, `.next`, etc. — directories that CLI `git status` skips at the
// directory level but that go-git currently descends into and hashes before
// dropping the results in excludeIgnoredChanges (worktree_status.go:175-177).
func setupIgnoredDirRepo(b *testing.B, tracked, untracked int) *Worktree {
	b.Helper()

	const ignoredDir = "vendor_ignored"

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)

	wt, err := repo.Worktree()
	require.NoError(b, err)

	for i := range tracked {
		path := filepath.Join("src", fmt.Sprintf("dir%02d", i%10), fmt.Sprintf("file%04d.go", i))
		require.NoError(b, wt.Filesystem.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(b, util.WriteFile(wt.Filesystem, path, []byte("package main\n"), 0o644))
	}

	require.NoError(b, util.WriteFile(wt.Filesystem, ".gitignore",
		[]byte(ignoredDir+"/\n"), 0o644))

	require.NoError(b, wt.AddGlob("src/*"))
	_, err = wt.Add(".gitignore")
	require.NoError(b, err)

	sig := &object.Signature{
		Name:  "Bench",
		Email: "bench@test.com",
		When:  time.Now().Add(-time.Hour), // older than index modtime so the metadata fast-path engages
	}
	_, err = wt.Commit("initial", &CommitOptions{Author: sig, Committer: sig})
	require.NoError(b, err)

	// Drop a large *gitignored* untracked tree. None of these files affect
	// status, but go-git lstat's and hashes each one.
	for i := range untracked {
		path := filepath.Join(ignoredDir, fmt.Sprintf("sub%02d", i%20), fmt.Sprintf("dep%05d.txt", i))
		require.NoError(b, wt.Filesystem.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(b, util.WriteFile(wt.Filesystem, path, []byte("ignored\n"), 0o644))
	}

	return wt
}

// BenchmarkStatusIgnoredDir measures the cost of running Status() over a tree
// that contains a large gitignored directory (e.g. node_modules-like).
//
// Compared to BenchmarkStatus, the only difference is that the extra files
// live in a directory listed in .gitignore. With the current implementation
// these files are still walked and hashed before being filtered out by
// excludeIgnoredChanges; CLI `git status` skips them entirely.
func BenchmarkStatusIgnoredDir(b *testing.B) {
	const tracked = 100

	cases := []struct {
		name      string
		untracked int
	}{
		{"BaselineNoIgnoredFiles", 0},
		{"IgnoredFiles_1k", 1000},
		{"IgnoredFiles_5k", 5000},
		{"IgnoredFiles_20k", 20000},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			wt := setupIgnoredDirRepo(b, tracked, tc.untracked)
			b.ResetTimer()
			for b.Loop() {
				s, err := wt.Status()
				if err != nil {
					b.Fatalf("status: %v", err)
				}
				if !s.IsClean() {
					b.Fatalf("expected clean status, got %v entries", len(s))
				}
			}
		})
	}
}
