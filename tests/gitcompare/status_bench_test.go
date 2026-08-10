// Package gitcompare_test measures go-git against the reference git binary on
// the same worktree.
//
// These are benchmarks rather than tests on purpose. There is no correct answer
// to assert: any threshold would be a flaky test on a shared runner, and
// nothing in CI passes -bench, so none of this runs there. Run it by hand when
// a change is expected to move Status cost:
//
//	go test ./tests/gitcompare/ -run '^$' -bench BenchmarkStatus -benchtime 20x
//
// Read the numbers with Baseline/EmptyRepo in view. A git invocation is a
// process, and process startup is a fixed cost go-git never pays: on macOS it
// is over 10ms, on Linux a few. Subtract the baseline from each git figure
// before concluding anything about the work either implementation does,
// otherwise go-git appears to win cases where it is in fact slower.
//
// Set GOGIT_COMPARE_REPO to an existing checkout to measure it as well, which
// is the quickest way to look at a worktree someone reports as slow:
//
//	GOGIT_COMPARE_REPO=~/src/some-repo go test ./tests/gitcompare/ \
//	    -run '^$' -bench BenchmarkStatus/Env -benchtime 20x
//
// A repository supplied that way is left exactly as it was found: Worktree.Status
// never writes the index, and git is invoked with --no-optional-locks so it does
// not refresh one either. Fixtures are throwaway and so are measured without
// that flag, which lets git cache stat data the way it normally would.
package gitcompare_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	git "github.com/go-git/go-git/v6"
)

// compareRepoEnv names an existing checkout to measure alongside the fixtures.
const compareRepoEnv = "GOGIT_COMPARE_REPO"

func requireGit(b *testing.B) {
	b.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		b.Skipf("git not found: %v", err)
	}
}

// runGit executes a git command that is expected to succeed.
func runGit(b *testing.B, dir string, args ...string) {
	b.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(b, err, "git %v: %s", args, out)
}

func write(b *testing.B, dir, rel, content string) {
	b.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(b, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(b, os.WriteFile(abs, []byte(content), 0o644))
}

// initRepo creates an empty repository. Fixtures are built with the git binary
// rather than with go-git so that both implementations read an index written
// the same way, and neither is measured against an index its own code produced.
func initRepo(b *testing.B) string {
	b.Helper()
	dir := filepath.Join(b.TempDir(), "repo")
	require.NoError(b, os.MkdirAll(dir, 0o755))
	runGit(b, dir, "-c", "init.defaultBranch=main", "init", "-q")
	runGit(b, dir, "config", "user.email", "bench@test.com")
	runGit(b, dir, "config", "user.name", "Bench")
	return dir
}

func commitAll(b *testing.B, dir string) {
	b.Helper()
	runGit(b, dir, "add", ".")
	runGit(b, dir, "commit", "-q", "-m", "initial")
}

// benchGitStatus measures `git status --porcelain`, including the process
// startup that Baseline/EmptyRepo exists to quantify.
//
// readOnly passes --no-optional-locks, which stops git refreshing the index it
// would normally rewrite. That keeps a repository supplied by the caller
// untouched, at the cost of understating git: it then repeats on every
// invocation the stat work a refreshed index would have saved it. Fixtures are
// throwaway, so they are measured without it and git gets its usual caching.
func benchGitStatus(dir string, readOnly bool) func(*testing.B) {
	args := []string{"-C", dir, "status", "--porcelain"}
	if readOnly {
		args = append([]string{"--no-optional-locks"}, args...)
	}

	return func(b *testing.B) {
		// One untimed invocation first. On a fresh fixture it populates the
		// index stat cache that steady-state use would already have, so the
		// measurement is not dominated by a cost paid once.
		if _, err := exec.Command("git", args...).Output(); err != nil {
			b.Fatalf("git status: %v", err)
		}

		for b.Loop() {
			if _, err := exec.Command("git", args...).Output(); err != nil {
				b.Fatalf("git status: %v", err)
			}
		}
	}
}

// benchGoGitStatus measures Worktree.Status on an already-open repository,
// which is how a library caller uses it.
func benchGoGitStatus(dir string) func(*testing.B) {
	return func(b *testing.B) {
		repo, err := git.PlainOpen(dir)
		require.NoError(b, err)
		b.Cleanup(func() { _ = repo.Close() })

		wt, err := repo.Worktree()
		require.NoError(b, err)

		// Matched against the untimed git invocation above.
		if _, err := wt.Status(); err != nil {
			b.Fatalf("status: %v", err)
		}

		for b.Loop() {
			if _, err := wt.Status(); err != nil {
				b.Fatalf("status: %v", err)
			}
		}
	}
}

// shape is a worktree layout to measure both implementations against.
type shape struct {
	name  string
	build func(b *testing.B) string
}

func shapes() []shape {
	return []shape{{
		// A rule naming a directory below the one it is declared in. Collecting
		// every pattern up front has to walk the ignored tree that the diff
		// walk then skips, so cost here grows with the size of that tree.
		name: "NestedIgnoredDir",
		build: func(b *testing.B) string {
			dir := initRepo(b)
			write(b, dir, ".gitignore", "e2e/artifacts/\n")
			for i := range 100 {
				write(b, dir, fmt.Sprintf("src/dir%02d/file%04d.go", i%10, i), "package main\n")
			}
			commitAll(b, dir)
			for i := range 2000 {
				write(b, dir, fmt.Sprintf("e2e/artifacts/sub%05d/artifact.txt", i), "ignored\n")
			}
			return dir
		},
	}, {
		// One .gitignore per directory and nothing excluded, the shape of a
		// monorepo where each package carries its own rules. Nothing can be
		// pruned, so what is left is the cost of the rules themselves.
		name: "ManyIgnoreFiles",
		build: func(b *testing.B) string {
			dir := initRepo(b)
			for i := range 500 {
				var rules strings.Builder
				for p := range 5 {
					fmt.Fprintf(&rules, "*.generated%02d\n", p)
				}
				write(b, dir, fmt.Sprintf("pkg%04d/.gitignore", i), rules.String())
				write(b, dir, fmt.Sprintf("pkg%04d/main.go", i), "package main\n")
			}
			commitAll(b, dir)
			return dir
		},
	}, {
		// No ignore rules at all: the floor for both implementations, dominated
		// by stat and hash work rather than by anything ignore-related.
		name: "LargeNoIgnoreFiles",
		build: func(b *testing.B) string {
			dir := initRepo(b)
			for i := range 5000 {
				write(b, dir, fmt.Sprintf("dir%d/file%04d.txt", i%20, i), "test content\n")
			}
			commitAll(b, dir)
			return dir
		},
	}}
}

func BenchmarkStatus(b *testing.B) {
	requireGit(b)

	// Measured first and deliberately named so it is hard to miss: this is what
	// a git invocation costs before it looks at a single file.
	b.Run("Baseline/EmptyRepo/git", benchGitStatus(initRepo(b), false))

	for _, s := range shapes() {
		dir := s.build(b)
		b.Run(s.name+"/git", benchGitStatus(dir, false))
		b.Run(s.name+"/go-git", benchGoGitStatus(dir))
	}

	// An existing checkout, if one was named. Its status need not be clean;
	// both sides simply report whatever is there. Measured read-only so the
	// repository is left exactly as it was found.
	if dir := os.Getenv(compareRepoEnv); dir != "" {
		abs, err := filepath.Abs(dir)
		require.NoError(b, err)
		b.Logf("%s=%s", compareRepoEnv, abs)
		b.Run("Env/git", benchGitStatus(abs, true))
		b.Run("Env/go-git", benchGoGitStatus(abs))
	}
}
