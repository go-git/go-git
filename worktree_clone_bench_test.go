package git

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/util"
	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

// BenchmarkCloneLargeRepo benchmarks cloning a repository with many files
// to exercise the checkoutChange codepath and resetWorktree performance.
func BenchmarkCloneLargeRepo(b *testing.B) {
	const (
		numFiles       = 2000
		numSubdirs     = 2
		numGoroutines  = 10
		filesPerSubdir = numFiles / numSubdirs
	)

	// Create a temporary directory for the source repository
	tmpDir := b.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")

	// Initialize source repository
	sourceRepo, err := PlainInit(sourceDir, false)
	require.NoError(b, err)

	sourceWt, err := sourceRepo.Worktree()
	require.NoError(b, err)

	// Create file content (same content for all files)
	content := []byte("test content for benchmark\n")

	// Create files in parallel using a pool of goroutines
	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	// Start worker goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				// Ensure subdirectory exists
				dir := filepath.Dir(filePath)
				err := sourceWt.Filesystem.MkdirAll(dir, 0755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				// Write file
				err = util.WriteFile(sourceWt.Filesystem, filePath, content, 0644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
	}

	// Generate file paths and send to workers
	for i := 0; i < numFiles; i++ {
		subdir := fmt.Sprintf("dir%d", i%numSubdirs)
		fileName := fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(subdir, fileName)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	// Manually add files to index in batches
	status, err := sourceWt.Status()
	require.NoError(b, err)

	idx, err := sourceRepo.Storer.Index()
	require.NoError(b, err)

	ignorePattern := make([]gitignore.Pattern, 0)

	// Add all files to index
	for filePath := range status {
		_, _, err := sourceWt.doAddFile(idx, status, filePath, ignorePattern)
		if err != nil {
			b.Fatalf("failed to add file %s to index: %v", filePath, err)
		}
	}

	// Write index
	err = sourceRepo.Storer.SetIndex(idx)
	require.NoError(b, err)

	// Commit all files
	sig := &object.Signature{
		Name:  "Benchmark",
		Email: "benchmark@test.com",
		When:  time.Now(),
	}
	_, err = sourceWt.Commit("Initial commit with many files", &CommitOptions{
		Author:    sig,
		Committer: sig,
	})
	require.NoError(b, err)

	// Run the benchmark: clone the repository with shared objects
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cloneDir := filepath.Join(tmpDir, fmt.Sprintf("clone-%d", i))
		_, err := PlainClone(cloneDir, &CloneOptions{
			URL:    sourceDir,
			Shared: true,
		})
		if err != nil {
			b.Fatalf("failed to clone repository: %v", err)
		}
	}
}

// BenchmarkCloneDeepRepo benchmarks cloning a repository with files in deep
// directory structures to demonstrate resetWorktree performance with nested paths.
func BenchmarkCloneDeepRepo(b *testing.B) {
	const (
		numFiles      = 2000
		dirDepth      = 5 // Nest directories 5 levels deep
		numGoroutines = 10
	)

	// Create a temporary directory for the source repository
	tmpDir := b.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")

	// Initialize source repository
	sourceRepo, err := PlainInit(sourceDir, false)
	require.NoError(b, err)

	sourceWt, err := sourceRepo.Worktree()
	require.NoError(b, err)

	// Create file content (same content for all files)
	content := []byte("test content for benchmark\n")

	// Create files in parallel using a pool of goroutines
	var wg sync.WaitGroup
	fileChan := make(chan string, numFiles)

	// Start worker goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				// Ensure subdirectory exists
				dir := filepath.Dir(filePath)
				err := sourceWt.Filesystem.MkdirAll(dir, 0755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}

				// Write file
				err = util.WriteFile(sourceWt.Filesystem, filePath, content, 0644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
	}

	// Generate file paths with deep nesting and send to workers
	for i := 0; i < numFiles; i++ {
		// Create path like: level0/level1/level2/level3/level4/file0001.txt
		pathParts := make([]string, dirDepth+1)
		for d := 0; d < dirDepth; d++ {
			pathParts[d] = fmt.Sprintf("level%d", d)
		}
		pathParts[dirDepth] = fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(pathParts...)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	// Manually add files to index in batches
	status, err := sourceWt.Status()
	require.NoError(b, err)

	idx, err := sourceRepo.Storer.Index()
	require.NoError(b, err)

	ignorePattern := make([]gitignore.Pattern, 0)

	// Add all files to index
	for filePath := range status {
		_, _, err := sourceWt.doAddFile(idx, status, filePath, ignorePattern)
		if err != nil {
			b.Fatalf("failed to add file %s to index: %v", filePath, err)
		}
	}

	// Write index
	err = sourceRepo.Storer.SetIndex(idx)
	require.NoError(b, err)

	// Commit all files
	sig := &object.Signature{
		Name:  "Benchmark",
		Email: "benchmark@test.com",
		When:  time.Now(),
	}
	_, err = sourceWt.Commit("Initial commit with many files", &CommitOptions{
		Author:    sig,
		Committer: sig,
	})
	require.NoError(b, err)

	// Run the benchmark: clone the repository with shared objects
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cloneDir := filepath.Join(tmpDir, fmt.Sprintf("clone-%d", i))
		_, err := PlainClone(cloneDir, &CloneOptions{
			URL:    sourceDir,
			Shared: true,
		})
		if err != nil {
			b.Fatalf("failed to clone repository: %v", err)
		}
	}
}
