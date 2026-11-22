package rad

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

// realRadicleHome returns the local Radicle home directory if it looks
// populated (has a storage/ tree), or "" otherwise.
func realRadicleHome(t *testing.T) string {
	t.Helper()

	home := os.Getenv("RAD_HOME")
	if home == "" {
		envVar := "HOME"
		if runtime.GOOS == "windows" {
			envVar = "USERPROFILE"
		}
		h := os.Getenv(envVar)
		if h == "" {
			return ""
		}
		home = filepath.Join(h, ".radicle")
	}

	if fi, err := os.Stat(filepath.Join(home, "storage")); err != nil || !fi.IsDir() {
		return ""
	}
	return home
}

// anyStoredRID returns the id of any repository present in home's storage
// tree, or "" if there is none. The fixtures here are whatever the machine
// running the test happens to have seeded, so they are discovered rather
// than hard-coded.
func anyStoredRID(t *testing.T, home string) string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(home, "storage"))
	require.NoError(t, err)

	for _, e := range entries {
		if e.IsDir() {
			return e.Name()
		}
	}
	return ""
}

// namespaceWithBranches returns the node id of a peer whose namespace in rid
// holds at least one branch, or "" if no peer does. Namespaces containing
// only refs/rad/*, refs/cobs/* and patch refs are rejected: every one of
// those is excluded from the ls-remote comparison, so a namespace without a
// plain branch would compare empty against empty and assert nothing.
func namespaceWithBranches(t *testing.T, home, rid string) string {
	t.Helper()

	out, err := exec.Command("git", "--git-dir="+filepath.Join(home, "storage", rid),
		"for-each-ref", "--format=%(refname)", "refs/namespaces/").CombinedOutput()
	require.NoErrorf(t, err, "git for-each-ref: %s", out)

	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		rest, ok := strings.CutPrefix(name, "refs/namespaces/")
		if !ok {
			continue
		}
		nid, ref, ok := strings.Cut(rest, "/")
		if !ok {
			continue
		}
		branch, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || strings.HasPrefix(branch, "patches/") {
			continue
		}
		return nid
	}
	return ""
}

// requireRadicleFixture skips the calling test unless this machine has a
// populated Radicle home and the git-remote-rad helper both integration
// tests shell out through.
func requireRadicleFixture(t *testing.T) string {
	t.Helper()

	home := realRadicleHome(t)
	if home == "" {
		t.Skip("no local Radicle storage found ($RAD_HOME or $HOME/.radicle/storage); skipping integration test")
	}
	if _, err := exec.LookPath("git-remote-rad"); err != nil {
		t.Skip("git-remote-rad not found in PATH; skipping integration test")
	}
	return home
}

// lsRemoteRefs runs `git ls-remote <url>` via the real git-remote-rad helper
// and returns the advertised references matching refs/heads/* or
// refs/tags/*, name -> hash.
func lsRemoteRefs(t *testing.T, url string) map[string]string {
	t.Helper()

	out, err := exec.Command("git", "ls-remote", url).CombinedOutput()
	require.NoErrorf(t, err, "git ls-remote %s: %s", url, out)

	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		require.Len(t, fields, 2, "unexpected ls-remote line: %q", line)
		hash, name := fields[0], fields[1]
		// refs/heads/patches/<id> is synthesized by git-remote-rad from
		// Radicle's collaborative objects (COBs); this transport does not
		// advertise it — see the package doc's "known deviations" section.
		if strings.HasPrefix(name, "refs/heads/patches/") {
			continue
		}
		if strings.HasPrefix(name, "refs/heads/") || strings.HasPrefix(name, "refs/tags/") {
			refs[name] = hash
		}
	}
	return refs
}

// transportRefs returns the refs/heads/* and refs/tags/* references
// advertised by this package's transport for u, name -> hash.
func transportRefs(t *testing.T, tr transport.Transport, u string) map[string]string {
	t.Helper()

	radURLValue := radURL(t, u)
	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     radURLValue,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	rr, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)

	refs := map[string]string{}
	for _, ref := range rr.References {
		name := ref.Name().String()
		// See lsRemoteRefs: refs/heads/patches/<id> is excluded from this
		// comparison since it is git-remote-rad-synthesized rather than
		// raw storage, so it is not expected to match a raw view either
		// way.
		if strings.HasPrefix(name, "refs/heads/patches/") {
			continue
		}
		if strings.HasPrefix(name, "refs/heads/") || strings.HasPrefix(name, "refs/tags/") {
			refs[name] = ref.Hash().String()
		}
	}
	return refs
}

// TestIntegration_MatchesGitRemoteRad ls-remotes a repository from the local
// Radicle storage through this package's transport and through the real
// git-remote-rad helper, and checks that refs/heads/* and refs/tags/* agree.
// It is skipped unless a populated Radicle home ($RAD_HOME or $HOME/.radicle)
// is present, and requires the `git` and `git-remote-rad` binaries in PATH.
func TestIntegration_MatchesGitRemoteRad(t *testing.T) {
	home := requireRadicleFixture(t)
	rid := anyStoredRID(t, home)
	if rid == "" {
		t.Skip("local Radicle storage holds no repositories; skipping integration test")
	}

	tr := NewTransport(Options{Home: home})

	t.Run("canonical", func(t *testing.T) {
		want := lsRemoteRefs(t, "rad://"+rid)
		require.NotEmpty(t, want, "fixture %s advertises no branches or tags, so this would assert nothing", rid)

		assert.Equal(t, want, transportRefs(t, tr, "rad://"+rid))
	})

	t.Run("namespaced", func(t *testing.T) {
		nid := namespaceWithBranches(t, home, rid)
		if nid == "" {
			t.Skipf("no peer namespace in %s holds a branch; skipping", rid)
		}

		want := lsRemoteRefs(t, "rad://"+rid+"/"+nid)
		require.NotEmpty(t, want, "namespace %s advertises no branches, so this would assert nothing", nid)

		assert.Equal(t, want, transportRefs(t, tr, "rad://"+rid+"/"+nid))
	})
}

// TestIntegration_Clone clones a real local Radicle repository through
// git.Clone using this package's transport, and checks that the cloned
// reference points at the commit the real helper advertises for it. Skipped
// under the same conditions as TestIntegration_MatchesGitRemoteRad.
func TestIntegration_Clone(t *testing.T) {
	home := requireRadicleFixture(t)
	rid := anyStoredRID(t, home)
	if rid == "" {
		t.Skip("local Radicle storage holds no repositories; skipping integration test")
	}

	want := lsRemoteRefs(t, "rad://"+rid)

	var branches []string
	for name := range want {
		if strings.HasPrefix(name, "refs/heads/") {
			branches = append(branches, name)
		}
	}
	if len(branches) == 0 {
		t.Skipf("fixture %s has no refs/heads/* to clone", rid)
	}
	sort.Strings(branches)
	target := "refs/heads/main"
	if _, ok := want[target]; !ok {
		target = branches[0]
	}

	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:           "rad://" + rid,
		ReferenceName: plumbing.ReferenceName(target),
		SingleBranch:  true,
		ClientOptions: []client.Option{
			client.WithTransport("rad", NewTransport(Options{Home: home})),
		},
	})
	require.NoError(t, err)
	defer func() { _ = repo.Close() }()

	ref, err := repo.Reference(plumbing.ReferenceName(target), true)
	require.NoError(t, err)
	assert.Equal(t, want[target], ref.Hash().String())
}
