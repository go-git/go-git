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

	wt, err := repo.Worktree()
	require.NoError(b, err)

	content := []byte("test content for benchmark\n")

	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				dir := filepath.Dir(filePath)
				err := wt.Filesystem.MkdirAll(dir, 0o755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				err = util.WriteFile(wt.Filesystem, filePath, content, 0o644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
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
			err := util.WriteFile(wt.Filesystem, filePath, modifiedContent, 0o644)
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
			for _, fileStatus := range status.Iter() {
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
