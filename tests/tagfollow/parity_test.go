package tagfollow_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// requireGit skips when no git binary is reachable.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	c := gitenv.Command("git", args...)
	if dir != "" {
		c.Dir = dir
	}
	return c
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := gitCmd(dir, args...).CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...).CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// buildServer builds a repository whose master and side branch diverge, with
// tags pointing to objects both reachable and unreachable from master:
//
//	main: c1 -- c2
//	side:  \ -- s1
//
//	lw-master     lightweight tag on c2          (reachable from master)
//	ann-master    annotated tag on c2            (reachable)
//	ann-blob      annotated tag on a blob in c2 (reachable)
//	lw-side       lightweight tag on s1         (unreachable from master)
//	ann-side      annotated tag on s1           (unreachable)
//
// The bare server copy is served by a git daemon. The work clone stays
// writable so the test can push more state mid-test.
func buildServer(t *testing.T) (root, work, serverURL string) {
	t.Helper()
	root = t.TempDir()
	work = filepath.Join(root, "work")
	server := filepath.Join(root, "server.git")

	git(t, "", "init", work)
	git(t, work, "symbolic-ref", "HEAD", "refs/heads/main")

	writeFile(t, work, "a.txt", "a\n")
	c1 := commit(t, work, "c1")
	require.NotEmpty(t, c1)

	writeFile(t, work, "a.txt", "a\nb\n")
	c2 := commit(t, work, "c2")

	git(t, work, "checkout", "-b", "side")
	writeFile(t, work, "side.txt", "s1\n")
	s1 := commit(t, work, "s1")

	git(t, work, "checkout", "main")

	git(t, work, "tag", "lw-master", c2)
	git(t, work, "tag", "-a", "-m", "annotated on master", "ann-master", c2)
	blob := gitOut(t, work, "hash-object", "a.txt")
	git(t, work, "tag", "-a", "-m", "annotated on blob", "ann-blob", blob)
	git(t, work, "tag", "lw-side", s1)
	git(t, work, "tag", "-a", "-m", "annotated on side", "ann-side", s1)

	git(t, "", "clone", "--bare", work, server)
	return root, work, startDaemon(t, root) + "server.git"
}

func startDaemon(t *testing.T, base string) string {
	t.Helper()
	port := freePort(t)
	daemon := gitenv.CommandContext(t.Context(), "git", "daemon",
		fmt.Sprintf("--base-path=%s", base), "--export-all",
		"--enable=receive-pack", "--enable=upload-archive", "--reuseaddr",
		fmt.Sprintf("--port=%d", port), "--max-connections=1", "--listen=127.0.0.1",
	)
	daemon.Cancel = func() error { return daemon.Process.Signal(os.Interrupt) }
	daemon.WaitDelay = 5 * time.Second
	require.NoError(t, daemon.Start())
	waited := make(chan error, 1)
	go func() { waited <- daemon.Wait() }()
	t.Cleanup(func() { <-waited })
	require.NoError(t, waitForPort(port))
	return fmt.Sprintf("git://127.0.0.1:%d/", port)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForPort(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				return conn.Close()
			}
		}
	}
}

// refsOf returns the sorted "name hash" pairs of a repository.
func refsOf(t *testing.T, repo string) []string {
	t.Helper()
	out := gitOut(t, repo, "show-ref")
	var refs []string
	if out == "" {
		return refs
	}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		require.Len(t, fields, 2, "unexpected show-ref line: %q", line)
		refs = append(refs, fields[1]+" "+fields[0])
	}
	sort.Strings(refs)
	return refs
}

// objectsOf returns the sorted set of objects reachable from all refs.
func objectsOf(t *testing.T, repo string) []string {
	t.Helper()
	out := gitOut(t, repo, "rev-list", "--objects", "--all")
	var objects []string
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		objects = append(objects, strings.TrimSpace(line))
	}
	sort.Strings(objects)
	return objects
}

// assertRepoParity asserts ours matches the git-CLI twin on refs, reachable
// objects, worktree cleanliness, and fsck.
func assertRepoParity(t *testing.T, ours, twin string) {
	t.Helper()
	git(t, ours, "fsck", "--no-dangling")
	require.ElementsMatch(t, refsOf(t, twin), refsOf(t, ours), "refs must match the git CLI twin")
	require.ElementsMatch(t, objectsOf(t, twin), objectsOf(t, ours), "reachable objects must match the git CLI twin")
}

// goGitFetch does a non-wildcard fetch of main with TagFollowing over wire
// protocol v, leaving the result in dir as a plain repository.
func goGitFetch(t *testing.T, url, dir string, v protocol.Version) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.Protocol.Version = v
	require.NoError(t, repo.SetConfig(cfg))
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name:  "origin",
		URLs:  []string{url},
		Fetch: []config.RefSpec{"+refs/heads/main:refs/remotes/origin/main"},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Fetch(&gogit.FetchOptions{
		RemoteName: "origin",
		Tags:       plumbing.TagFollowing,
	}))
}

// gitTwinFetch does the same fetch with the git CLI over protocol v.
func gitTwinFetch(t *testing.T, url, dir string, v protocol.Version) {
	t.Helper()
	git(t, "", "init", dir)
	git(t, dir, "remote", "add", "origin", url)
	git(t, dir, "-c", fmt.Sprintf("protocol.version=%s", v), "fetch", "origin",
		"+refs/heads/main:refs/remotes/origin/main")
}

// TestNonWildcardFetchFollowsReachableTags is the parity test for the
// tag-following fix: a non-wildcard fetch with TagFollowing must follow
// exactly the tags whose targets are reachable from the fetched refs, and no
// others, matching the git CLI. Annotated tags pointing to fetched objects
// require the second backfill_tags fetch round; tags pointing to unfetched
// objects must not be followed (the reachability gate).
func TestNonWildcardFetchFollowsReachableTags(t *testing.T) {
	t.Parallel()
	requireGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("git for windows has issues with the git:// protocol. See https://github.com/git-for-windows/git/issues/907")
	}

	for _, tc := range []struct {
		version protocol.Version
		name    string
	}{
		{protocol.V0, "v0"},
		{protocol.V1, "v1"},
		{protocol.V2, "v2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, _, url := buildServer(t)

			twin := filepath.Join(root, "twin")
			ours := filepath.Join(root, "ours")
			gitTwinFetch(t, url, twin, tc.version)
			goGitFetch(t, url, ours, tc.version)

			assertRepoParity(t, ours, twin)

			// lw-side and ann-side point at the side branch, which the
			// non-wildcard fetch of main never reached: neither client may
			// create those tag refs.
			for _, bad := range []string{"refs/tags/lw-side", "refs/tags/ann-side"} {
				_, err := exec.Command("git", "-C", ours, "rev-parse", bad).CombinedOutput()
				require.Error(t, err, "go-git must not follow %s (target unreachable)", bad)
				_, err = exec.Command("git", "-C", twin, "rev-parse", bad).CombinedOutput()
				require.Error(t, err, "git twin must not follow %s either", bad)
			}
		})
	}
}
