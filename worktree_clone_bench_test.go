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

// BenchmarkCloneLargeRepo benchmarks cloning a repository with many files
// to exercise the checkoutChange codepath and FindEntry performance.
func BenchmarkCloneLargeRepo(b *testing.B) {
	const (
		numFiles       = 2000
		numSubdirs     = 2
		numGoroutines  = 10
		filesPerSubdir = numFiles / numSubdirs
	)

	tmpDir := b.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")

	sourceRepo, err := PlainInit(sourceDir, false)
	require.NoError(b, err)

	sourceWt, err := sourceRepo.Worktree()
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
				err := sourceWt.Filesystem.MkdirAll(dir, 0o755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}
				err = util.WriteFile(sourceWt.Filesystem, filePath, content, 0o644)
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

	// Add all files to index using AddGlob for each directory as Add is too slow at the moment.
	for i := range numSubdirs {
		err = sourceWt.AddGlob(fmt.Sprintf("dir%d/*", i))
		require.NoError(b, err)
	}

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

	i := 0
	for b.Loop() {
		cloneDir := filepath.Join(tmpDir, fmt.Sprintf("clone-%d", i))
		_, err := PlainClone(cloneDir, &CloneOptions{
			URL:    sourceDir,
			Shared: true,
		})
		if err != nil {
			b.Fatalf("failed to clone repository: %v", err)
		}
		i++
	}
}

// BenchmarkCloneDeepRepo benchmarks cloning a repository with files in deep
// directory structures to better demonstrate FindEntry caching benefits.
func BenchmarkCloneDeepRepo(b *testing.B) {
	const (
		numFiles      = 2000
		dirDepth      = 5
		numGoroutines = 10
	)

	tmpDir := b.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")

	sourceRepo, err := PlainInit(sourceDir, false)
	require.NoError(b, err)

	sourceWt, err := sourceRepo.Worktree()
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
				err := sourceWt.Filesystem.MkdirAll(dir, 0o755)
				if err != nil {
					b.Errorf("failed to create directory %s: %v", dir, err)
					continue
				}
				err = util.WriteFile(sourceWt.Filesystem, filePath, content, 0o644)
				if err != nil {
					b.Errorf("failed to write file %s: %v", filePath, err)
				}
			}
		}()
	}

	for i := range numFiles {
		pathParts := make([]string, dirDepth+1)
		for d := range dirDepth {
			pathParts[d] = fmt.Sprintf("level%d", d)
		}
		pathParts[dirDepth] = fmt.Sprintf("file%04d.txt", i)
		filePath := filepath.Join(pathParts...)
		fileChan <- filePath
	}
	close(fileChan)
	wg.Wait()

	// Add all files to index using AddGlob as Add is too slow at the moment.
	err = sourceWt.AddGlob("level0/*/*/*/*/*")
	require.NoError(b, err)

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

	i := 0
	for b.Loop() {
		cloneDir := filepath.Join(tmpDir, fmt.Sprintf("clone-%d", i))
		_, err := PlainClone(cloneDir, &CloneOptions{
			URL:    sourceDir,
			Shared: true,
		})
		if err != nil {
			b.Fatalf("failed to clone repository: %v", err)
		}
		i++
	}
}
