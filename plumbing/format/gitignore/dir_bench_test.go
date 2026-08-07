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
	populateTree(b, root, dirs)

	if withIgnoreFiles {
		require.NoError(b, os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755))
		require.NoError(b, os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("*.log\n"), 0o644))
		require.NoError(b, os.WriteFile(filepath.Join(root, gitignoreFile), []byte("*.tmp\n"), 0o644))
	}

	return root
}

// populateTree creates `dirs` directories under base, each holding one file.
func populateTree(b *testing.B, base string, dirs int) {
	b.Helper()

	for top := range dirs / 10 {
		for sub := range 10 {
			dir := filepath.Join(base, fmt.Sprintf("top%03d", top), fmt.Sprintf("sub%02d", sub))
			require.NoError(b, os.MkdirAll(dir, 0o755))
			require.NoError(b, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x\n"), 0o644))
		}
	}
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

	// The two cases above declare only file globs, so nothing is ever pruned
	// and the whole tree is walked either way. These two isolate the pruning
	// path: an identical tree is excluded by a rule naming a direct child in
	// one case and a grandchild in the other. Depth1 prunes after listing the
	// root alone; Nested must also list outer before it can prune
	// outer/ignored, so each case costs one ReadDir per ancestor down to the
	// pruned directory. Before patterns were inherited through the recursion
	// only Depth1 pruned at all, and Nested walked every directory.
	for _, tc := range []struct {
		name, rule string
	}{
		{"PrunedPattern/Depth1", "outer/\n"},
		{"PrunedPattern/Nested", "outer/ignored/\n"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			fs := osfs.New(setupPrunedTree(b, dirs, tc.rule), osfs.WithBoundOS())
			for b.Loop() {
				ps, err := ReadPatterns(fs, nil)
				if err != nil {
					b.Fatalf("ReadPatterns: %v", err)
				}
				if len(ps) != 1 {
					b.Fatalf("expected 1 pattern, got %d", len(ps))
				}
			}
		})
	}
}

// setupPrunedTree builds the same tree as setupReadPatternsTree but nested
// under outer/ignored/, with rule as the sole root .gitignore entry.
func setupPrunedTree(b *testing.B, dirs int, rule string) string {
	b.Helper()

	root := b.TempDir()
	populateTree(b, filepath.Join(root, "outer", "ignored"), dirs)
	require.NoError(b, os.WriteFile(filepath.Join(root, gitignoreFile), []byte(rule), 0o644))

	return root
}
