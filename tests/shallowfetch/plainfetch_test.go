package shallowfetch_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// goGitShallowClone brings dir to the state git clone --depth 1 of url
// leaves behind, speaking wire protocol v. A fresh v2 clone is a
// PlainClone; v0 has no PlainClone protocol override, so it is init,
// configured, remote, fetch, and the clone-equivalent refs a clone leaves
// behind (local main, origin/HEAD, HEAD on main).
func goGitShallowClone(t *testing.T, url, dir string, v protocol.Version) *gogit.Repository {
	t.Helper()

	var repo *gogit.Repository
	var err error
	if v == protocol.V2 {
		// Single-branch + tag-following, mirroring git clone --depth 1's
		// defaults exactly: git fetches only HEAD's branch and only tags
		// reachable from it. go-git's PlainClone default fetches every ref
		// and every tag, which for this fixture pulls in the feature branch
		// tip (f2) and the v1 tag (c3) as extra shallow roots -- correct
		// for an all-refs clone, but not what the git CLI twin does, so the
		// two would not be comparable. Matching git's defaults keeps the
		// parity assertion meaningful.
		//
		// ReferenceName is refs/heads/main rather than HEAD so the clone
		// creates refs/remotes/origin/main (which resetToOriginMain resolves);
		// PlainClone with HEAD leaves only refs/remotes/origin/HEAD. The
		// origin/HEAD symref git clone leaves behind is added below, the
		// way the v0 path does.
		repo, err = gogit.PlainClone(dir, &gogit.CloneOptions{
			URL:           url,
			Depth:         1,
			SingleBranch:  true,
			ReferenceName: plumbing.ReferenceName("refs/heads/main"),
			Tags:          plumbing.TagFollowing,
		})
		require.NoError(t, err)
		require.NoError(t, repo.Storer.SetReference(plumbing.NewSymbolicReference(
			plumbing.ReferenceName("refs/remotes/origin/HEAD"),
			plumbing.ReferenceName("refs/remotes/origin/main"),
		)))
	} else {
		repo, err = gogit.PlainInit(dir, false)
		require.NoError(t, err)
		cfg, err := repo.Config()
		require.NoError(t, err)
		cfg.Protocol.Version = v
		require.NoError(t, repo.SetConfig(cfg))
		_, err = repo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{url},
			// Single-branch, mirroring git clone --depth 1's default over
			// this daemon: the twin ends up with only main, without feature.
			Fetch: []config.RefSpec{
				"+refs/heads/main:refs/remotes/origin/main",
			},
		})
		require.NoError(t, err)
		err = repo.Fetch(&gogit.FetchOptions{Depth: 1})
		require.NoError(t, err)

		// Leave behind what git clone --depth 1 leaves: a local main at
		// the remote tip, origin/HEAD pointing at it, and HEAD on main.
		tip, err := repo.ResolveRevision(plumbing.Revision("refs/remotes/origin/main"))
		require.NoError(t, err)
		require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference(
			plumbing.ReferenceName("refs/heads/main"), *tip,
		)))
		require.NoError(t, repo.Storer.SetReference(plumbing.NewSymbolicReference(
			plumbing.ReferenceName("refs/remotes/origin/HEAD"),
			plumbing.ReferenceName("refs/remotes/origin/main"),
		)))
		require.NoError(t, repo.Storer.SetReference(plumbing.NewSymbolicReference(
			plumbing.HEAD, plumbing.ReferenceName("refs/heads/main"),
		)))
	}

	return repo
}

// resetToOriginMain consumes a fetch the way the git CLI twin does:
// `git checkout origin/main` detaches HEAD at the remote tip and leaves
// local branches where the clone left them.
func resetToOriginMain(t *testing.T, repo *gogit.Repository) {
	t.Helper()
	hash, err := repo.ResolveRevision(plumbing.Revision("refs/remotes/origin/main"))
	require.NoError(t, err)
	// Checkout, not Reset, mirrors the twin's `git checkout origin/main`:
	// it detaches HEAD at the remote tip, leaves refs/heads/main at the
	// clone-time commit, and updates the worktree to the target tree --
	// including removing files the target tree no longer has. A bare
	// Reset after manually detaching HEAD would diff the new HEAD's tree
	// against itself (an empty diff) and leave stale files behind.
	w, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, w.Checkout(&gogit.CheckoutOptions{Hash: *hash, Force: true}))
}

// advanceServer pushes one more commit onto the server's main, after the
// client's initial clone, so the fetch under test has something to fetch.
// The push goes to the bare server path directly, the way the fixture's
// doc comment describes; the daemon serves the same directory from disk.
func advanceServer(t *testing.T, work string) string {
	t.Helper()
	writeFile(t, work, "d.txt", "d\n")
	hash := commit(t, work, "advance")
	git(t, work, "push", filepath.Join(filepath.Dir(work), "server.git"), "main")
	return hash
}

// TestPlainFetchAfterShallowClone is the #305 scenario: a depth-1 clone
// followed by a fetch with no depth must behave like git's, which
// advertises the shallow boundary and gets a pack computed for a client
// that lacks the boundary's ancestry (fetch-pack.c:436-437).
func TestPlainFetchAfterShallowClone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		version protocol.Version
	}{
		{protocol.V0},
		{protocol.V2},
	} {
		t.Run(fmt.Sprintf("protocol=%v", tc.version), func(t *testing.T) {
			t.Parallel()
			requireGit(t)
			root, work, url := buildServer(t)

			// Both clients start from the same server state: the git CLI
			// twin, then go-git, each as a depth-1 clone.
			twin := filepath.Join(root, "twin")
			git(t, "", "clone", "--depth", "1", url, twin)

			ours := filepath.Join(root, "ours")
			repo := goGitShallowClone(t, url, ours, tc.version)

			// The server gains one commit; both clients plain-fetch it,
			// with no depth, and consume the result.
			advanceServer(t, work)

			git(t, twin, "fetch", "origin")
			git(t, twin, "checkout", "origin/main")

			require.NoError(t, repo.Fetch(&gogit.FetchOptions{RemoteName: "origin"}))
			resetToOriginMain(t, repo)

			assertRepoParity(t, ours, twin)
		})
	}
}
