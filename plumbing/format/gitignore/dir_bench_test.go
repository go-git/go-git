package gitignore

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"
)

// setupReadPatternsTree builds a directory tree with `dirs` nested
// directories. With gitignore, a .gitignore is placed at the root and a
// .git/info/exclude file is present, as in a regular repository worktree.
func setupReadPatternsTree(b *testing.B, dirs int, withIgnoreFiles bool) string {
	b.Helper()

	root := b.TempDir()

	for top := range dirs / 10 {
		for sub := range 10 {
			dir := filepath.Join(root, fmt.Sprintf("top%03d", top), fmt.Sprintf("sub%02d", sub))
			require.NoError(b, os.MkdirAll(dir, 0o755))
			require.NoError(b, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x\n"), 0o644))
		}
	}

	if withIgnoreFiles {
		require.NoError(b, os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755))
		require.NoError(b, os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("*.log\n"), 0o644))
		require.NoError(b, os.WriteFile(filepath.Join(root, gitignoreFile), []byte("*.tmp\n"), 0o644))
	}

	return root
}

// BenchmarkReadPatterns measures the cost of collecting ignore patterns
// over a tree whose directories contain no ignore files - the common case
// on worktrees like home directories, where blindly attempting to open
// .gitignore and .git/info/exclude per directory dominated Status() CPU.
func BenchmarkReadPatterns(b *testing.B) {
	const dirs = 1100

	b.Run("NoIgnoreFiles", func(b *testing.B) {
		fs := osfs.New(setupReadPatternsTree(b, dirs, false), osfs.WithBoundOS())
		b.ResetTimer()
		for b.Loop() {
			ps, err := ReadPatterns(fs, nil)
			if err != nil {
				b.Fatalf("ReadPatterns: %v", err)
			}
			if len(ps) != 0 {
				b.Fatalf("expected no patterns, got %d", len(ps))
			}
		}
	})

	b.Run("RootIgnoreFiles", func(b *testing.B) {
		fs := osfs.New(setupReadPatternsTree(b, dirs, true), osfs.WithBoundOS())
		b.ResetTimer()
		for b.Loop() {
			ps, err := ReadPatterns(fs, nil)
			if err != nil {
				b.Fatalf("ReadPatterns: %v", err)
			}
			if len(ps) != 2 {
				b.Fatalf("expected 2 patterns, got %d", len(ps))
			}
		}
	})
}
