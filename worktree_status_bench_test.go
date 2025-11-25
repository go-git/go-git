package git

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/util"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

// BenchmarkStatusClean benchmarks Status() on a clean repository with many files.
// This represents the worst-case scenario for the current implementation where
// every file's hash is computed unnecessarily since nothing has changed.
func BenchmarkStatusClean(b *testing.B) {
	const (
		numFiles      = 2000
		numSubdirs    = 10
		numGoroutines = 10
	)

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)

	wt, err := repo.Worktree()
	require.NoError(b, err)

	content := []byte("test content for benchmark\n")

	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				dir := filepath.Dir(filePath)
				err := wt.Filesystem.MkdirAll(dir, 0755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				err = util.WriteFile(wt.Filesystem, filePath, content, 0644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
	}

	for i := 0; i < numFiles; i++ {
		subdir := fmt.Sprintf("dir%d", i%numSubdirs)
		fileName := fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(subdir, fileName)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	for i := 0; i < numSubdirs; i++ {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status, err := wt.Status()
		if err != nil {
			b.Fatalf("failed to get status: %v", err)
		}
		if !status.IsClean() {
			b.Fatalf("expected clean status, got: %v", status)
		}
	}
}

// BenchmarkStatusModified benchmarks Status() on a repository with some modified files.
// This represents a more realistic scenario where a small percentage of files have changed.
func BenchmarkStatusModified(b *testing.B) {
	const (
		numFiles        = 2000
		numSubdirs      = 10
		numGoroutines   = 10
		modifiedPercent = 1
	)

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)

	wt, err := repo.Worktree()
	require.NoError(b, err)

	content := []byte("test content for benchmark\n")

	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				dir := filepath.Dir(filePath)
				err := wt.Filesystem.MkdirAll(dir, 0755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				err = util.WriteFile(wt.Filesystem, filePath, content, 0644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
	}

	for i := 0; i < numFiles; i++ {
		subdir := fmt.Sprintf("dir%d", i%numSubdirs)
		fileName := fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(subdir, fileName)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	for i := 0; i < numSubdirs; i++ {
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

	numModified := (numFiles * modifiedPercent) / 100
	if numModified == 0 {
		numModified = 1
	}
	modifiedContent := []byte("modified content\n")
	for i := 0; i < numModified; i++ {
		subdir := fmt.Sprintf("dir%d", i%numSubdirs)
		fileName := fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(subdir, fileName)
		err = util.WriteFile(wt.Filesystem, filePath, modifiedContent, 0644)
		require.NoError(b, err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
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

// BenchmarkStatusLarge benchmarks Status() on a large repository with many files.
// This simulates a more realistic large repository scenario.
func BenchmarkStatusLarge(b *testing.B) {
	const (
		numFiles      = 5000
		numSubdirs    = 20
		numGoroutines = 10
	)

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)

	wt, err := repo.Worktree()
	require.NoError(b, err)

	content := []byte("test content for benchmark\n")

	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				dir := filepath.Dir(filePath)
				err := wt.Filesystem.MkdirAll(dir, 0755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				err = util.WriteFile(wt.Filesystem, filePath, content, 0644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
	}

	for i := 0; i < numFiles; i++ {
		subdir := fmt.Sprintf("dir%d", i%numSubdirs)
		fileName := fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(subdir, fileName)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	for i := 0; i < numSubdirs; i++ {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status, err := wt.Status()
		if err != nil {
			b.Fatalf("failed to get status: %v", err)
		}
		if !status.IsClean() {
			b.Fatalf("expected clean status, got: %v", status)
		}
	}
}
