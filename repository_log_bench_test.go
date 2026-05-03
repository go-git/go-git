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

// BenchmarkLogWithPathFilterAndDateRange benchmarks git.Log with both a
// PathFilter and a Since/Until date range, using two sub-benchmarks that
// show the asymmetry of fixing the filter ordering:
//
//   - DateFiltersHeavily: narrow date window eliminates 90% of commits before
//     any path filter runs. Currently all commits do an expensive tree-diff
//     before the cheap date check is applied. Fixing the order yields a large
//     speedup proportional to how selective the date range is.
//
//   - PathFiltersHeavily: date range covers all commits (never filters). Only
//     every 20th commit touches tracked.txt, so the path filter does real work.
//     Fixing the order adds a cheap date check before each tree-diff but
//     changes nothing about how many tree-diffs occur — overhead is negligible.
func BenchmarkLogWithPathFilterAndDateRange(b *testing.B) {
	const numCommits = 200

	// buildRepo creates a repo where every 20th commit (0, 20, 40, …) touches
	// tracked.txt and the rest touch noise.txt.
	buildRepo := func(b *testing.B) (*Repository, time.Time) {
		b.Helper()
		repo, err := PlainInit(filepath.Join(b.TempDir(), "repo"), false)
		require.NoError(b, err)

		wt, err := repo.Worktree()
		require.NoError(b, err)

		base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

		require.NoError(b, util.WriteFile(wt.Filesystem, "noise.txt", []byte("init\n"), 0o644))
		require.NoError(b, util.WriteFile(wt.Filesystem, "tracked.txt", []byte("init\n"), 0o644))
		_, err = wt.Add("noise.txt")
		require.NoError(b, err)
		_, err = wt.Add("tracked.txt")
		require.NoError(b, err)

		for i := range numCommits {
			if i%20 == 0 {
				require.NoError(b, util.WriteFile(wt.Filesystem, "tracked.txt", fmt.Appendf(nil, "tracked %d\n", i), 0o644))
				_, err = wt.Add("tracked.txt")
			} else {
				require.NoError(b, util.WriteFile(wt.Filesystem, "noise.txt", fmt.Appendf(nil, "noise %d\n", i), 0o644))
				_, err = wt.Add("noise.txt")
			}
			require.NoError(b, err)

			sig := object.Signature{Name: "Bench", Email: "bench@example.com", When: base.Add(time.Duration(i) * 24 * time.Hour)}
			_, err = wt.Commit(fmt.Sprintf("commit %d", i), &CommitOptions{Author: &sig, Committer: &sig})
			require.NoError(b, err)
		}
		return repo, base
	}

	runLog := func(b *testing.B, repo *Repository, opts *LogOptions) {
		b.Helper()
		iter, err := repo.Log(opts)
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		if err := iter.ForEach(func(_ *object.Commit) error { count++; return nil }); err != nil {
			b.Fatal(err)
		}
		iter.Close()
		b.ReportMetric(float64(count), "commits/op")
	}

	// DateFiltersHeavily: narrow date window covers only ~10% of commits
	// (days 90–110 out of 200), but tracked.txt changes in every commit so the
	// path filter never rejects anything.
	// Currently: all 200 commits do a tree-diff; 180 are then cut by the date check.
	// With the fix: only 20 commits reach the tree-diff.
	// Expected benefit: large speedup (~10×).
	b.Run("DateFiltersHeavily", func(b *testing.B) {
		repo, err := PlainInit(filepath.Join(b.TempDir(), "repo"), false)
		require.NoError(b, err)

		wt, err := repo.Worktree()
		require.NoError(b, err)

		base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(b, util.WriteFile(wt.Filesystem, "tracked.txt", []byte("init\n"), 0o644))
		_, err = wt.Add("tracked.txt")
		require.NoError(b, err)

		for i := range numCommits {
			require.NoError(b, util.WriteFile(wt.Filesystem, "tracked.txt", fmt.Appendf(nil, "tracked %d\n", i), 0o644))
			_, err = wt.Add("tracked.txt")
			require.NoError(b, err)

			sig := object.Signature{Name: "Bench", Email: "bench@example.com", When: base.Add(time.Duration(i) * 24 * time.Hour)}
			_, err = wt.Commit(fmt.Sprintf("commit %d", i), &CommitOptions{Author: &sig, Committer: &sig})
			require.NoError(b, err)
		}

		since := base.Add(90 * 24 * time.Hour)
		until := base.Add(110 * 24 * time.Hour)

		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			runLog(b, repo, &LogOptions{
				Since:      &since,
				Until:      &until,
				PathFilter: func(path string) bool { return path == "tracked.txt" },
			})
		}
	})

	// PathFiltersHeavily: date range spans the entire history (never filters),
	// but only every 20th commit touches tracked.txt (~95% rejection by path).
	// Currently: 200 tree-diffs, path rejects 190.
	// With the fix: 200 cheap date checks (all pass), then 200 tree-diffs — same work.
	// Expected benefit: negligible change.
	b.Run("PathFiltersHeavily", func(b *testing.B) {
		repo, base := buildRepo(b)

		since := base.Add(-1 * 24 * time.Hour)
		until := base.Add((numCommits + 1) * 24 * time.Hour)

		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			runLog(b, repo, &LogOptions{
				Since:      &since,
				Until:      &until,
				PathFilter: func(path string) bool { return path == "tracked.txt" },
			})
		}
	})
}
