package git

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/object"
)

// setupBenchmarkRepo creates a test repository with the specified number of files.
// It returns the worktree for benchmarking.
func setupBenchmarkRepo(b *testing.B, numFiles, numSubdirs, numGoroutines int) *Worktree {
	b.Helper()

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)
	b.Cleanup(func() { _ = repo.Close() })

	wt, err := repo.Worktree()
	require.NoError(b, err)

	content := []byte("test content for benchmark\n")

	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	for range numGoroutines {
		wg.Go(func() {
			for filePath := range fileChan {
				dir := filepath.Dir(filePath)
				err := wt.Filesystem().MkdirAll(dir, 0o755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				err = util.WriteFile(wt.Filesystem(), filePath, content, 0o644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		})
	}

	for i := range numFiles {
		subdir := fmt.Sprintf("dir%d", i%numSubdirs)
		fileName := fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(subdir, fileName)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	for i := range numSubdirs {
		err = wt.AddGlob(fmt.Sprintf("dir%d/*", i))
		require.NoError(b, err)
	}

	sig := &object.Signature{
		Name:  "Benchmark",
		Email: "benchmark@test.com",
		When:  time.Now(),
	}
	_, err = wt.Commit("Initial commit with many files", &CommitOptions{
		Author:    sig,
		Committer: sig,
	})
	require.NoError(b, err)

	return wt
}

// BenchmarkStatus benchmarks Status() on a repository with 2000 files.
// It includes sub-benchmarks for clean and modified scenarios.
func BenchmarkStatus(b *testing.B) {
	const (
		numFiles      = 2000
		numSubdirs    = 10
		numGoroutines = 10
	)

	wt := setupBenchmarkRepo(b, numFiles, numSubdirs, numGoroutines)

	b.Run("Clean", benchmarkStatusClean(wt))
	b.Run("Modified", benchmarkStatusModified(wt, numFiles, numSubdirs))
}

// benchmarkStatusClean returns a benchmark function for testing Status() on a clean repository.
// This represents the worst-case scenario for the current implementation where
// every file's hash is computed unnecessarily since nothing has changed.
func benchmarkStatusClean(wt *Worktree) func(b *testing.B) {
	return func(b *testing.B) {
		for b.Loop() {
			status, err := wt.Status()
			if err != nil {
				b.Fatalf("failed to get status: %v", err)
			}
			if !status.IsClean() {
				b.Fatalf("expected clean status, got: %v", status)
			}
		}
	}
}

// benchmarkStatusModified returns a benchmark function for testing Status() on a repository
// with some modified files. This represents a more realistic scenario where a small
// percentage of files have changed.
func benchmarkStatusModified(wt *Worktree, numFiles, numSubdirs int) func(b *testing.B) {
	return func(b *testing.B) {
		const modifiedPercent = 1

		numModified := (numFiles * modifiedPercent) / 100
		if numModified == 0 {
			numModified = 1
		}
		modifiedContent := []byte("modified content\n")
		for i := range numModified {
			subdir := fmt.Sprintf("dir%d", i%numSubdirs)
			fileName := fmt.Sprintf("file%04d.txt", i)
			filePath := filepath.Join(subdir, fileName)
			err := util.WriteFile(wt.Filesystem(), filePath, modifiedContent, 0o644)
			require.NoError(b, err)
		}

		for b.Loop() {
			status, err := wt.Status()
			if err != nil {
				b.Fatalf("failed to get status: %v", err)
			}
			if status.IsClean() {
				b.Fatalf("expected modified status, got clean")
			}
			modCount := 0
			for _, fileStatus := range status {
				if fileStatus.Worktree == Modified {
					modCount++
				}
			}
			if modCount != numModified {
				b.Fatalf("expected %d modified files, got %d", numModified, modCount)
			}
		}
	}
}

// BenchmarkStatusLarge benchmarks Status() on a large repository with 5000 files.
func BenchmarkStatusLarge(b *testing.B) {
	const (
		numFiles      = 5000
		numSubdirs    = 20
		numGoroutines = 10
	)

	wt := setupBenchmarkRepo(b, numFiles, numSubdirs, numGoroutines)

	b.Run("Clean", benchmarkStatusClean(wt))
}

// setupIgnoredDirRepo builds a repo with `tracked` source files committed, a
// `.gitignore` excluding `ignoredDir`, and `untracked` files dropped into
// `ignoredDir`. The ignored directory is a stand-in for `node_modules`,
// `vendor`, `.next`, etc. — directories that CLI `git status` skips at the
// directory level and that go-git also skips via the filesystem walker's
// IgnoreMatcher.
func setupIgnoredDirRepo(b *testing.B, tracked, untracked int) *Worktree {
	b.Helper()

	const ignoredDir = "vendor_ignored"

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)
	b.Cleanup(func() { _ = repo.Close() })

	wt, err := repo.Worktree()
	require.NoError(b, err)

	for i := range tracked {
		path := filepath.Join("src", fmt.Sprintf("dir%02d", i%10), fmt.Sprintf("file%04d.go", i))
		require.NoError(b, wt.Filesystem().MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(b, util.WriteFile(wt.Filesystem(), path, []byte("package main\n"), 0o644))
	}

	require.NoError(b, util.WriteFile(wt.Filesystem(), ".gitignore",
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
	// status, and the walker skips the directory entirely.
	for i := range untracked {
		path := filepath.Join(ignoredDir, fmt.Sprintf("sub%02d", i%20), fmt.Sprintf("dep%05d.txt", i))
		require.NoError(b, wt.Filesystem().MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(b, util.WriteFile(wt.Filesystem(), path, []byte("ignored\n"), 0o644))
	}

	return wt
}

// BenchmarkStatusIgnoredDir measures the cost of running Status() over a tree
// that contains a large gitignored directory (e.g. node_modules-like).
//
// Compared to BenchmarkStatus, the only difference is that the extra files
// live in a directory listed in .gitignore. The filesystem walker's
// IgnoreMatcher skips the directory at enumeration time, so cost should stay
// roughly flat as the number of ignored files grows.
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
