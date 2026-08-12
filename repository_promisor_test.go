package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

// The partial-clone tests below use the git binary to build the fixture and to
// judge the result. Both matter: only real git produces the on-disk shape of a
// partial clone (promisor-marked packs plus genuinely absent objects), and only
// real git decides whether an absence reads as promised or as corruption. A
// pure go-git assertion would happily accept a repository that git refuses to
// gc, which is exactly the bug these cover.

func requireGitBinary(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("oracle disabled: git not found: %v", err)
	}
}

// partialCloneMinGit is the first Git release whose clone accepts --filter.
// Partial clone landed in 2.19.0, so on anything older the fixture cannot be
// built at all: `git clone --filter=blob:none` fails with "unknown option".
// The Git Compatibility workflow builds 2.11.0, which is exactly that case.
//
// A version comparison is the right test here, unlike for a behavioural bug a
// vendor might backport: the option either exists in the CLI or it does not,
// and no distribution adds partial clone to 2.11.0.
var partialCloneMinGit = [2]int{2, 19}

// requireGitPartialClone skips unless the git binary can build the fixture
// these tests need.
func requireGitPartialClone(t *testing.T) {
	t.Helper()
	requireGitBinary(t)

	out, err := exec.Command("git", "version").Output()
	if err != nil {
		t.Skipf("cannot determine git version: %v", err)
	}

	major, minor, ok := parseGitMajorMinor(string(out))
	if !ok {
		t.Skipf("cannot parse git version from %q", strings.TrimSpace(string(out)))
	}

	wantMajor, wantMinor := partialCloneMinGit[0], partialCloneMinGit[1]
	if major < wantMajor || (major == wantMajor && minor < wantMinor) {
		t.Skipf("git %d.%d has no clone --filter, so a partial clone cannot be built (needs %d.%d+)",
			major, minor, wantMajor, wantMinor)
	}
}

// parseGitMajorMinor extracts the major and minor version from `git version`
// output, which carries a variable number of trailing components and, on some
// platforms, a vendor suffix. ok is false when the output cannot be read, which
// callers treat as "unknown, skip" rather than assuming a modern Git.
func parseGitMajorMinor(out string) (major, minor int, ok bool) {
	fields := strings.Fields(out)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return 0, 0, false
	}

	parts := strings.Split(fields[2], ".")
	if len(parts) < 2 {
		return 0, 0, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}

	return major, minor, true
}

// git runs a git command that is expected to succeed and returns its output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// protocol.file.allow keeps file:// transport usable across git versions
	// that restrict it by default.
	full := append([]string{"-C", dir, "-c", "protocol.file.allow=always"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// gitAllowFail runs a git command and returns its output and whether it
// succeeded, for the cases where the failure is the thing under test.
func gitAllowFail(t *testing.T, dir string, args ...string) (string, bool) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "protocol.file.allow=always"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	return string(out), err == nil
}

// newPartialClone builds a real partial clone with the given filter and returns
// its path. The source history rewrites the same file on every commit, so each
// commit contributes a distinct blob and the clone is left genuinely missing
// objects rather than trivially complete.
//
// The clone is made with --no-checkout so no lazy backfill happens: checking out
// a worktree would fetch the objects it needs and quietly heal the very state
// under test.
func newPartialClone(t *testing.T, filter string) string {
	t.Helper()

	base := t.TempDir()
	src := filepath.Join(base, "src.git")
	seed := filepath.Join(base, "seed")
	dst := filepath.Join(base, "clone")

	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.MkdirAll(seed, 0o755))

	out, err := exec.Command("git", "init", "-q", "--bare", src).CombinedOutput()
	require.NoError(t, err, "git init --bare: %s", out)
	git(t, src, "config", "uploadpack.allowFilter", "true")

	out, err = exec.Command("git", "init", "-q", seed).CombinedOutput()
	require.NoError(t, err, "git init: %s", out)
	git(t, seed, "config", "user.email", "test@example.com")
	git(t, seed, "config", "user.name", "test")

	for _, content := range []string{"one", "two", "three", "four"} {
		require.NoError(t, os.WriteFile(filepath.Join(seed, "file.txt"), []byte(content+"\n"), 0o644))
		git(t, seed, "add", ".")
		git(t, seed, "commit", "-qm", content)
	}
	git(t, seed, "branch", "-M", "main")
	git(t, seed, "remote", "add", "origin", src)
	git(t, seed, "push", "-q", "origin", "main")

	out, err = exec.Command("git", "-c", "protocol.file.allow=always", "clone", "-q",
		"--filter="+filter, "--no-checkout", "file://"+src, dst).CombinedOutput()
	require.NoError(t, err, "git clone --filter=%s: %s", filter, out)

	// Sanity-check the fixture really is a partial clone with absent objects,
	// so a later clean fsck means the markers did their job rather than that
	// there was nothing to promise.
	require.NotEmpty(t, promisorMarkers(t, dst), "fixture should have promisor-marked packs")
	require.NotZero(t, missingObjects(t, dst), "fixture should be missing objects")
	requireFsckClean(t, dst)

	return dst
}

func packDir(dir string) string {
	return filepath.Join(dir, ".git", "objects", "pack")
}

func promisorMarkers(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(packDir(dir), "*.promisor"))
	require.NoError(t, err)
	return m
}

// missingObjects counts the objects git reaches from the refs but does not have.
func missingObjects(t *testing.T, dir string) int {
	t.Helper()
	out := git(t, dir, "rev-list", "--objects", "--all", "--missing=print")
	var n int
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "?") {
			n++
		}
	}
	return n
}

// requireFsckClean asserts git considers every absence promised. fsck reports
// "broken link"/"missing blob" for an object that is absent without a promisor
// pack vouching for it, and stays silent when one does.
func requireFsckClean(t *testing.T, dir string) {
	t.Helper()
	out, ok := gitAllowFail(t, dir, "fsck")
	assert.True(t, ok, "git fsck failed: %s", out)
	assert.NotContains(t, out, "broken link", "git fsck: %s", out)
	assert.NotContains(t, out, "missing blob", "git fsck: %s", out)
}

// requireNoOrphanMarkers asserts every .promisor still has the pack it belongs
// to. An orphan claims a pack that is gone, so the objects it vouched for are no
// longer understood as promised.
func requireNoOrphanMarkers(t *testing.T, dir string) {
	t.Helper()
	for _, marker := range promisorMarkers(t, dir) {
		pack := strings.TrimSuffix(marker, ".promisor") + ".pack"
		_, err := os.Stat(pack)
		assert.NoError(t, err, "orphaned marker %s has no pack", filepath.Base(marker))
	}
}

// partialCloneFilters covers the two shapes that leave different object types
// absent: blob:none withholds blobs, which the walk reaches through tree
// entries, while tree:0 withholds the trees themselves, which it reaches by
// loading them. Each exercises a different tolerance path.
var partialCloneFilters = []string{"blob:none", "tree:0"}

// TestRepackObjectsOnPartialClone covers repacking a partial clone. It used to
// fail outright with "object not found", because the walk assumed every object
// a tree names is present, which a filtered clone breaks.
//
// The pack it produces has to be promisor-marked in turn: it carries the objects
// that reference the withheld ones, so leaving it unmarked would turn those
// absences into corruption and cost the repository its ability to gc.
func TestRepackObjectsOnPartialClone(t *testing.T) {
	t.Parallel()
	requireGitPartialClone(t)

	for _, filter := range partialCloneFilters {
		t.Run(filter, func(t *testing.T) {
			t.Parallel()

			dir := newPartialClone(t, filter)
			before := missingObjects(t, dir)

			r, err := PlainOpen(dir)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			require.NoError(t, r.RepackObjects(&RepackConfig{}))

			assert.NotEmpty(t, promisorMarkers(t, dir),
				"the repacked pack must stay promisor-marked, or the objects the remote withheld read as corruption")
			requireNoOrphanMarkers(t, dir)
			assert.Equal(t, before, missingObjects(t, dir), "repacking must not change which objects are absent")

			// The judgement that matters: git still accepts the repository, and
			// can still gc it. Before the fix gc died with "unable to read".
			requireFsckClean(t, dir)
			out, ok := gitAllowFail(t, dir, "gc", "--prune=now")
			assert.True(t, ok, "git gc failed after repack: %s", out)
			requireFsckClean(t, dir)
		})
	}
}

// TestPruneOnPartialClone covers pruning a partial clone, which shares the walk
// that RepackObjects used to fail in.
func TestPruneOnPartialClone(t *testing.T) {
	t.Parallel()
	requireGitPartialClone(t)

	for _, filter := range partialCloneFilters {
		t.Run(filter, func(t *testing.T) {
			t.Parallel()

			dir := newPartialClone(t, filter)
			before := missingObjects(t, dir)

			r, err := PlainOpen(dir)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			require.NoError(t, r.Prune(PruneOptions{Handler: r.DeleteObject}))

			assert.Equal(t, before, missingObjects(t, dir), "pruning must not drop a promised object")
			requireNoOrphanMarkers(t, dir)
			requireFsckClean(t, dir)
		})
	}
}

// TestPartialCloneMarkersSurviveObjectWrites covers the ordinary case of go-git
// writing into a partial clone, which must leave the existing markers alone.
func TestPartialCloneMarkersSurviveObjectWrites(t *testing.T) {
	t.Parallel()
	requireGitPartialClone(t)

	dir := newPartialClone(t, "blob:none")
	before := promisorMarkers(t, dir)

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	obj := r.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("written by go-git\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = r.Storer.SetEncodedObject(obj)
	require.NoError(t, err)

	assert.ElementsMatch(t, before, promisorMarkers(t, dir))
	requireFsckClean(t, dir)
}

// TestParseGitMajorMinor covers the shapes `git version` emits. Misreading it
// would skip the partial-clone tests on a Git that supports them, or run them
// on one that does not and fail building the fixture.
func TestParseGitMajorMinor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		out   string
		major int
		minor int
		ok    bool
	}{
		{name: "release", out: "git version 2.19.0\n", major: 2, minor: 19, ok: true},
		{name: "patch release", out: "git version 2.50.1\n", major: 2, minor: 50, ok: true},
		{name: "vendor suffix", out: "git version 2.50.1 (Apple Git-155)\n", major: 2, minor: 50, ok: true},
		{name: "build from master", out: "git version 2.55.0.525.g2c78326f81\n", major: 2, minor: 55, ok: true},
		{name: "windows suffix", out: "git version 2.51.0.windows.1\n", major: 2, minor: 51, ok: true},
		{name: "legacy", out: "git version 2.11.0\n", major: 2, minor: 11, ok: true},
		{name: "empty", out: ""},
		{name: "truncated", out: "git version\n"},
		{name: "unexpected prefix", out: "hg version 2.19.0\n"},
		{name: "single component", out: "git version 2\n"},
		{name: "non-numeric", out: "git version next.0\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			major, minor, ok := parseGitMajorMinor(tc.out)
			if !tc.ok {
				assert.False(t, ok, "parseGitMajorMinor(%q) should report failure", tc.out)
				return
			}
			require.True(t, ok, "parseGitMajorMinor(%q) should succeed", tc.out)
			assert.Equal(t, tc.major, major)
			assert.Equal(t, tc.minor, minor)
		})
	}
}
