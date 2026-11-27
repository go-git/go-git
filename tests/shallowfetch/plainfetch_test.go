package shallowfetch_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// runPlainFetchParity depth-1 clones the server with go-git and with git,
// lets moveServer change the server's main, and then fetches without a
// depth on both sides. Git sends its shallow lines on every fetch, so
// upload-pack grafts the boundary and its have no longer implies the
// commits behind it (fetch-pack.c find_common and add_shallow_requests).
func runPlainFetchParity(t *testing.T, moveServer func(t *testing.T, work, server string)) {
	t.Helper()
	skipOnWindows(t)
	requireGit(t)

	for _, v := range []protocol.Version{protocol.V0, protocol.V2} {
		t.Run(fmt.Sprintf("protocol=%v", v), func(t *testing.T) {
			t.Parallel()
			root, work, url := buildServer(t)

			twin := filepath.Join(root, "twin")
			git(t, "", "clone", "--depth", "1", url, twin)

			ours := filepath.Join(root, "ours")
			repo, err := gogit.PlainClone(ours, &gogit.CloneOptions{
				URL:           url,
				Depth:         1,
				SingleBranch:  true,
				ReferenceName: plumbing.NewBranchReferenceName("main"),
				Tags:          plumbing.TagFollowing,
			})
			require.NoError(t, err)
			require.NoError(t, repo.Storer.SetReference(plumbing.NewSymbolicReference(
				plumbing.NewRemoteHEADReferenceName("origin"),
				plumbing.NewRemoteReferenceName("origin", "main"),
			)))
			cfg, err := repo.Config()
			require.NoError(t, err)
			cfg.Protocol.Version = v
			require.NoError(t, repo.SetConfig(cfg))

			moveServer(t, work, filepath.Join(root, "server.git"))

			git(t, twin, "fetch", "origin")
			require.NoError(t, repo.Fetch(&gogit.FetchOptions{}))

			assertRepoParity(t, ours, twin)
		})
	}
}

// TestPlainFetchMergesFromBelowBoundary is the shape of #305: the server
// merges a branch that forked below the client's boundary, and the fetch
// must bring in the history the boundary cut off.
func TestPlainFetchMergesFromBelowBoundary(t *testing.T) {
	t.Parallel()
	runPlainFetchParity(t, func(t *testing.T, work, server string) {
		root := gitOut(t, work, "rev-list", "--max-parents=0", "main")
		git(t, work, "checkout", "-b", "below", root)
		writeFile(t, work, "below.txt", "below\n")
		commit(t, work, "below")
		git(t, work, "checkout", "main")
		git(t, work, "merge", "--no-ff", "below", "-m", "merge below")
		git(t, work, "push", server, "main")
	})
}

// TestPlainFetchAfterRewindBelowBoundary is the shape of #1443: the server
// force-pushes main back to a commit below the client's boundary, which the
// fetch must deliver.
func TestPlainFetchAfterRewindBelowBoundary(t *testing.T) {
	t.Parallel()
	runPlainFetchParity(t, func(t *testing.T, work, server string) {
		git(t, work, "push", "--force", server, "main~2:main")
	})
}
