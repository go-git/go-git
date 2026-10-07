package shallowfetch_test

import (
	"fmt"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/stretchr/testify/require"
)

// runScenario drives both clients through the same steps and asserts parity.
// setup runs after the server's initial depth-1 clone on both sides; it
// advances the server and returns the git-CLI arguments for the twin's
// fetch, and the fetch overrides for go-git's.
func runScenario(t *testing.T, name string,
	setup func(t *testing.T, work string) (gitArgs []string, fetch func(*gogit.FetchOptions)),
) {
	for _, v := range []protocol.Version{protocol.V0, protocol.V2} {
		t.Run(fmt.Sprintf("%s/protocol=%v", name, v), func(t *testing.T) {
			t.Parallel()
			requireGit(t)
			root, work, url := buildServer(t)

			// Both clients depth-1 clone the SAME server state BEFORE setup
			// mutates the server, so the two clones are comparable.
			twin := filepath.Join(root, "twin")
			git(t, "", "clone", "--depth", "1", url, twin)

			ours := filepath.Join(root, "ours")
			repo := goGitShallowClone(t, url, ours, v)

			gitArgs, goFetch := setup(t, work)

			git(t, twin, append([]string{"fetch", "origin"}, gitArgs...)...)
			git(t, twin, "checkout", "origin/main")

			fetchOpts := &gogit.FetchOptions{RemoteName: "origin"}
			if goFetch != nil {
				goFetch(fetchOpts)
			}
			require.NoError(t, repo.Fetch(fetchOpts))
			resetToOriginMain(t, repo)

			assertRepoParity(t, ours, twin)
		})
	}
}

// TestDeepenFetch: clone at depth 1, fetch with depth 3. The boundary must
// move to the new frontier, and the deepened history must be complete to
// the requested depth on both clients.
func TestDeepenFetch(t *testing.T) {
	runScenario(t, "deepen", func(t *testing.T, work string) ([]string, func(*gogit.FetchOptions)) {
		return []string{"--depth", "3"},
			func(o *gogit.FetchOptions) { o.Depth = 3 }
	})
}

// TestShallowRefreshFetch: clone at depth 1, fetch again at depth 1 after
// the server advanced. This is the re-shallow path a periodic refresher
// takes.
func TestShallowRefreshFetch(t *testing.T) {
	runScenario(t, "refresh", func(t *testing.T, work string) ([]string, func(*gogit.FetchOptions)) {
		advanceServer(t, work)
		return []string{"--depth", "1"},
			func(o *gogit.FetchOptions) { o.Depth = 1 }
	})
}

// TestFetchAfterRewind: the server force-pushes main back to an older
// commit after the client's clone. The fetch must converge both clients to
// the rewound tip without object-not-found (the #1443 shape).
func TestFetchAfterRewind(t *testing.T) {
	runScenario(t, "rewind", func(t *testing.T, work string) ([]string, func(*gogit.FetchOptions)) {
		older := gitOut(t, work, "rev-parse", "HEAD~2")
		git(t, work, "reset", "--hard", older)
		// Push from the fixture work clone, not the test process's CWD:
		// a bare-path push with no -C runs in the CWD and would push this
		// repository's own refs to the server, corrupting the scenario.
		git(t, work, "push", "--force", filepath.Join(filepath.Dir(work), "server.git"), "main:main")
		return []string{"--force"},
			func(o *gogit.FetchOptions) { o.Force = true }
	})
}

// TestFetchAcrossMergeBoundary: the initial depth-1 clone lands on the
// merge commit M; a plain fetch must deliver M's second-parent history
// (the feature branch), which the depth-1 pack never contained.
func TestFetchAcrossMergeBoundary(t *testing.T) {
	runScenario(t, "merge-boundary", func(t *testing.T, work string) ([]string, func(*gogit.FetchOptions)) {
		return nil, nil
	})
}
