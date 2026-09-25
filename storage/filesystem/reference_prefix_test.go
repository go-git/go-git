package filesystem_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)
	return strings.TrimSpace(string(out))
}

func newPrefixFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init", "-q")
	if _, err := os.Stat(filepath.Join(dir, ".git", "reftable")); err == nil {
		t.Skip("git defaults to the reftable backend")
	}

	commit := func(msg string) string {
		runGit(t, dir, "-c", "user.name=a", "-c", "user.email=a@example.com",
			"commit", "-q", "--allow-empty", "-m", msg)
		return runGit(t, dir, "rev-parse", "HEAD")
	}
	a := commit("a")
	b := commit("b")

	for name, hash := range map[string]string{
		"refs/heads/main":                a,
		"refs/heads/fe/one":              a,
		"refs/heads/feature":             a,
		"refs/heads/fix":                 b,
		"refs/heads/a-c":                 a,
		"refs/heads/a.b":                 b,
		"refs/heads/a/b":                 a,
		"refs/heads/a0":                  b,
		"refs/remotes/origin/main":       a,
		"refs/remotes/origin/topic":      b,
		"refs/remotes/origin-other/main": b,
		"refs/tags/v1":                   a,
	} {
		runGit(t, dir, "update-ref", name, hash)
	}
	runGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	runGit(t, dir, "-c", "user.name=a", "-c", "user.email=a@example.com",
		"tag", "-a", "-m", "v2", "v2", a)

	return dir
}

// gitForEachRef lists the references matching patterns as
// "<name> <object> <symref target>", the same shape as prefixRefs.
func gitForEachRef(t *testing.T, dir string, patterns ...string) []string {
	t.Helper()
	args := append([]string{"for-each-ref", "--format=%(refname) %(objectname) %(symref)"}, patterns...)
	cmd := gitenv.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)
	if len(out) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

func prefixRefs(t *testing.T, sto *filesystem.Storage, prefix string) []string {
	t.Helper()
	iter, err := storer.IterReferencesWithPrefix(sto, prefix)
	require.NoError(t, err)

	var refs []string
	require.NoError(t, iter.ForEach(func(r *plumbing.Reference) error {
		hash := r.Hash()
		if r.Type() == plumbing.SymbolicReference {
			resolved, err := storer.ResolveReference(sto, r.Name())
			require.NoError(t, err)
			hash = resolved.Hash()
		}
		refs = append(refs, r.Name().String()+" "+hash.String()+" "+r.Target().String())
		return nil
	}))
	return refs
}

func goGitPackRefs(t *testing.T, dir string) {
	t.Helper()
	sto := filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()
	require.NoError(t, sto.PackRefs())
}

// git reads packed-refs written by go-git the same as the loose refs they
// replace, peeling annotated tags itself since no peeled trait is claimed, and
// git refs verify, where available, confirms the sorted claim.
func TestPackRefsIsReadByGit(t *testing.T) {
	t.Parallel()
	dir := newPrefixFixture(t)
	listRefs := func() string {
		return runGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(*objectname) %(symref)")
	}
	showRefs := func() string {
		return runGit(t, dir, "show-ref", "--dereference")
	}
	wantList, wantShow := listRefs(), showRefs()
	require.Contains(t, wantShow, "refs/tags/v2^{}")

	goGitPackRefs(t, dir)

	content, err := os.ReadFile(filepath.Join(dir, ".git", "packed-refs"))
	require.NoError(t, err)
	header, _, _ := strings.Cut(string(content), "\n")
	assert.Equal(t, "# pack-refs with: sorted ", header)
	assert.Equal(t, wantList, listRefs())
	assert.Equal(t, wantShow, showRefs())

	// git refs verify arrived in 2.47; older git lacks "refs" entirely, or
	// only knows "refs migrate" and prints its usage.
	verify := gitenv.Command("git", "-C", dir, "refs", "verify")
	if out, err := verify.CombinedOutput(); err != nil &&
		(strings.Contains(string(out), "is not a git command") || strings.Contains(string(out), "usage")) {
		t.Log("git refs verify is unavailable")
	} else {
		assert.NoError(t, err, "git refs verify: %s", out)
	}

	runGit(t, dir, "pack-refs", "--all")
	assert.Equal(t, wantList, listRefs())
}

func TestIterReferencesWithPrefixMatchesGit(t *testing.T) {
	t.Parallel()

	scenarios := map[string]func(t *testing.T, dir string){
		"Loose": func(*testing.T, string) {},
		"Packed": func(t *testing.T, dir string) {
			runGit(t, dir, "pack-refs", "--all")
		},
		"LooseOverridesPacked": func(t *testing.T, dir string) {
			runGit(t, dir, "pack-refs", "--all")
			runGit(t, dir, "update-ref", "refs/heads/feature", "refs/heads/fix")
			runGit(t, dir, "update-ref", "refs/remotes/origin/new", "refs/heads/main")
		},
		// Removing a loose ref exposes its packed value.
		"LooseRemovedOverPacked": func(t *testing.T, dir string) {
			runGit(t, dir, "pack-refs", "--all")
			runGit(t, dir, "update-ref", "refs/heads/fix", "refs/heads/main")
			require.NoError(t, os.Remove(filepath.Join(dir, ".git", "refs", "heads", "fix")))
		},
		// git skips broken loose refs with a warning instead of failing,
		// still lets them hide the packed ref of the same name, and does not
		// treat ".*" or "*.lock" entries as refs.
		"BrokenLoose": func(t *testing.T, dir string) {
			runGit(t, dir, "pack-refs", "--all")
			head := runGit(t, dir, "rev-parse", "refs/heads/main")
			for name, content := range map[string]string{
				"refs/heads/empty":               "",
				"refs/heads/garbage":             "garbage\n",
				"refs/heads/zero":                strings.Repeat("0", len(head)) + "\n",
				"refs/heads/spaced":              "  " + head + "\n",
				"refs/heads/fix":                 "",
				"refs/heads/main.lock":           head + "\n",
				"refs/heads/.hidden":             head + "\n",
				"refs/remotes/origin/topic.lock": head + "\n",
				"refs/heads/trailing":            head + " trailing\n",
			} {
				path := filepath.Join(dir, ".git", filepath.FromSlash(name))
				require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
			}
		},
		"GoGitPackRefs": func(t *testing.T, dir string) {
			goGitPackRefs(t, dir)
		},
		// Git accepts a packed-refs file without the sorted trait and sorts
		// it in memory, so its order must not matter.
		"UnsortedPacked": func(t *testing.T, dir string) {
			runGit(t, dir, "pack-refs", "--all")
			path := filepath.Join(dir, ".git", "packed-refs")
			content, err := os.ReadFile(path)
			require.NoError(t, err)

			lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
			// Older git, such as 2.11, writes the header without the sorted trait.
			require.True(t, strings.HasPrefix(lines[0], "# pack-refs with: "), lines[0])
			var records []string
			for _, line := range lines[1:] {
				if strings.HasPrefix(line, "^") {
					records[len(records)-1] += "\n" + line
					continue
				}
				records = append(records, line)
			}
			slices.Reverse(records)

			unsorted := "# pack-refs with: peeled fully-peeled \n" + strings.Join(records, "\n") + "\n"
			require.NoError(t, os.WriteFile(path, []byte(unsorted), 0o644))
		},
	}

	for name, setup := range scenarios {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := newPrefixFixture(t)
			setup(t, dir)
			sto := filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
			defer func() { _ = sto.Close() }()

			// git for-each-ref matches a pattern ending in "/" as a plain
			// prefix, and sorts by refname, so its output is the expected
			// output, order included.
			for _, prefix := range []string{
				"refs/", "refs/heads/", "refs/heads/fe/", "refs/remotes/origin/",
				"refs/remotes/origin-other/", "refs/tags/", "refs/nope/",
			} {
				assert.Equal(t, gitForEachRef(t, dir, prefix), prefixRefs(t, sto, prefix), prefix)
			}

			// Without the trailing "/", for-each-ref matches whole path
			// components only; the globs spell out the byte-wise prefix.
			for _, prefix := range []string{"refs/heads/a", "refs/heads/fe", "refs/remotes/origin"} {
				assert.Equal(t, gitForEachRef(t, dir, prefix+"*", prefix+"*/**"), prefixRefs(t, sto, prefix), prefix)
			}
		})
	}
}

// The storer prefix is byte-wise, like the prefix iterators of Git's
// reference backends, while for-each-ref treats a pattern without a trailing
// slash as whole path components:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/ref-filter.c#L2695-L2724
func TestIterReferencesWithPrefixIsByteWiseUnlikeForEachRef(t *testing.T) {
	t.Parallel()
	dir := newPrefixFixture(t)
	sto := filesystem.NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	names := func(lines []string) []string {
		names := make([]string, 0, len(lines))
		for _, line := range lines {
			name, _, _ := strings.Cut(line, " ")
			names = append(names, name)
		}
		return names
	}

	assert.ElementsMatch(t, []string{
		"refs/remotes/origin/HEAD", "refs/remotes/origin/main", "refs/remotes/origin/topic",
	}, names(gitForEachRef(t, dir, "refs/remotes/origin")))
	assert.ElementsMatch(t, []string{
		"refs/remotes/origin/HEAD", "refs/remotes/origin/main", "refs/remotes/origin/topic",
		"refs/remotes/origin-other/main",
	}, names(prefixRefs(t, sto, "refs/remotes/origin")))
}
