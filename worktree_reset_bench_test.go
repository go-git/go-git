package git

import (
	"testing"
)

// BenchmarkResetHardIgnoredDir measures the cost of HardReset over a tree
// that contains a large gitignored directory (e.g. node_modules-like).
//
// resetWorktreeToTree walks the worktree to discover tracked files that are
// missing or stale on disk. Without the IgnoreMatcher optimization, this walk
// descends into every gitignored subtree before the loop body discards the
// resulting Delete actions. With the matcher engaged, the entire ignored
// subtree is skipped at enumeration time, so cost stays roughly flat as the
// number of ignored files grows.
//
// Mirrors BenchmarkStatusIgnoredDir to make the speedup directly comparable
// across the two operations.
func BenchmarkResetHardIgnoredDir(b *testing.B) {
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
			ref, err := wt.r.Head()
			if err != nil {
				b.Fatalf("head: %v", err)
			}
			head := ref.Hash()
			b.ResetTimer()
			for b.Loop() {
				if err := wt.Reset(&ResetOptions{Mode: HardReset, Commit: head}); err != nil {
					b.Fatalf("reset: %v", err)
				}
			}
		})
	}
}
