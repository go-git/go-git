package git

import (
	"fmt"
	"path/filepath"
	"strings"
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

// setupNestedIgnoredDirRepo is setupIgnoredDirRepo with the ignored tree one
// level deeper, so the root rule names a grandchild rather than a direct
// child. The distinction is invisible to the diff walk, which prunes the
// directory either way, but it decides whether collecting the ignore patterns
// beforehand has to descend through the tree first.
//
// dirs, not files, is the parameter that matters: pattern collection costs one
// ReadDir per directory, so a wide shallow tree of empty-ish directories is
// what separates the approaches.
func setupNestedIgnoredDirRepo(b *testing.B, tracked, dirs int) *Worktree {
	b.Helper()

	const ignoredDir = "e2e/artifacts"

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

	// A wide gitignored tree. None of it affects status.
	for i := range dirs {
		path := filepath.Join(ignoredDir, fmt.Sprintf("sub%05d", i), "artifact.txt")
		require.NoError(b, wt.Filesystem().MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(b, util.WriteFile(wt.Filesystem(), path, []byte("ignored\n"), 0o644))
	}

	return wt
}

// BenchmarkStatusNestedIgnoredDir measures Status over a worktree whose
// ignored tree sits below the directory named by the rule. BenchmarkStatusIgnoredDir
// cannot show this: its rule names a direct child of the root, which every
// implementation prunes, and its ignored tree is only 21 directories wide.
func BenchmarkStatusNestedIgnoredDir(b *testing.B) {
	const tracked = 100

	for _, dirs := range []int{200, 2000} {
		b.Run(fmt.Sprintf("IgnoredDirs_%d", dirs), func(b *testing.B) {
			wt := setupNestedIgnoredDirRepo(b, tracked, dirs)
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

// setupManyIgnoreFilesRepo builds a worktree where every directory declares
// its own .gitignore, the shape of a monorepo in which each package carries
// its own rules. Everything is tracked and nothing is excluded wholesale, so
// pruning excluded subtrees cannot help: both the pattern collection and the
// diff walk have to visit every directory regardless.
//
// What is left is the cost the flat pattern list imposes. Collecting patterns
// eagerly yields one list holding every rule in the repository, and each entry
// the walk considers is tested against all of them. Evaluating per directory
// instead tests an entry only against the rules on its own ancestor chain, so
// the two diverge as the number of ignore files grows rather than as the size
// of any one ignored tree grows.
func setupManyIgnoreFilesRepo(b *testing.B, dirs, patternsPerDir int) *Worktree {
	b.Helper()

	tmpDir := b.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	repo, err := PlainInit(repoDir, false)
	require.NoError(b, err)
	b.Cleanup(func() { _ = repo.Close() })

	wt, err := repo.Worktree()
	require.NoError(b, err)

	for i := range dirs {
		dir := fmt.Sprintf("pkg%04d", i)
		require.NoError(b, wt.Filesystem().MkdirAll(dir, 0o755))

		// Rules that match nothing present, so every entry is tested against
		// every one of them without any short-circuiting on a hit.
		var rules strings.Builder
		for p := range patternsPerDir {
			fmt.Fprintf(&rules, "*.generated%02d\n", p)
		}
		require.NoError(b, util.WriteFile(wt.Filesystem(),
			filepath.Join(dir, ".gitignore"), []byte(rules.String()), 0o644))
		require.NoError(b, util.WriteFile(wt.Filesystem(),
			filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	}

	require.NoError(b, wt.AddWithOptions(&AddOptions{All: true}))

	sig := &object.Signature{
		Name:  "Bench",
		Email: "bench@test.com",
		When:  time.Now().Add(-time.Hour), // older than index modtime so the metadata fast-path engages
	}
	_, err = wt.Commit("initial", &CommitOptions{Author: sig, Committer: sig})
	require.NoError(b, err)

	return wt
}

// BenchmarkStatusManyIgnoreFiles measures the case pruning cannot improve:
// many ignore files, none of them excluding a subtree. It isolates the cost of
// holding every rule in the repository in one list.
func BenchmarkStatusManyIgnoreFiles(b *testing.B) {
	const patternsPerDir = 5

	for _, dirs := range []int{50, 500} {
		b.Run(fmt.Sprintf("IgnoreFiles_%d", dirs), func(b *testing.B) {
			wt := setupManyIgnoreFilesRepo(b, dirs, patternsPerDir)
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
