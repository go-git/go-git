package http

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"

	internalhttp "github.com/go-git/go-git/v6/internal/server/http"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

// run executes a git (or other) command in the given dir and fails the test on error.
// Output is captured for diagnostics.
func run(t testing.TB, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s %v failed in %s: %s", name, args, dir, string(out))
}

// runV2 is like run but forces GIT_PROTOCOL=version=2. This ensures the git CLI
// uses (and the test exercises) the v2 paths on our server. Without this, the test
// could pass even if the v2 wire protocol was broken (as happened with manual
// tests that had history and triggered haves+ready negotiation).
func runV2(t testing.TB, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_PROTOCOL=version=2")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s %v failed in %s: %s", name, args, dir, string(out))
}

// requireGitV2 skips the test unless the git CLI in PATH supports protocol v2
// (introduced in git 2.18). Against older clients the GIT_PROTOCOL=version=2
// hint is ignored and the wire silently falls back to v0, so these v2 e2e tests
// would not exercise what they claim to.
func requireGitV2(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found in PATH; required for e2e")
	}
	fields := strings.Fields(gitOut(t, t.TempDir(), "version")) // e.g. "git version 2.39.2"
	if len(fields) < 3 {
		t.Skipf("cannot parse git version %q", strings.Join(fields, " "))
	}
	parts := strings.SplitN(fields[2], ".", 3)
	major, errMaj := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if errMaj != nil || major < 2 || (major == 2 && minor < 18) {
		t.Skipf("git %s does not support protocol v2 (need >= 2.18)", fields[2])
	}
}

// setupGoGitBackendServer starts an HTTP server using go-git's internal backend
// (no cgi, no git-http-backend) serving a filesystem loader rooted at tmp.
// The bare repo "testrepo.git" is first created and seeded using the git CLI
// (as required) so that git CLI clients can speak real protocol (v2 for upload-pack)
// against our server implementation.
func setupGoGitBackendServer(t testing.TB) (baseURL, repoName string) {
	t.Helper()

	tmp := t.TempDir()
	repoName = "testrepo.git"
	repoDir := filepath.Join(tmp, repoName)
	require.NoError(t, os.MkdirAll(repoDir, 0o755))

	// Seed with git CLI (init, commit, push to bare) — exercises git CLI + our server later.
	run(t, tmp, "git", "init", "--bare", repoName)
	// Pin the bare repo's default branch to main regardless of the ambient
	// init.defaultBranch (CI may default to master); otherwise its HEAD dangles
	// at a branch we never push and clients can't resolve the default branch.
	run(t, repoDir, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	run(t, repoDir, "git", "config", "http.receivepack", "true")
	run(t, repoDir, "git", "config", "http.uploadpack", "true")

	work := filepath.Join(tmp, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))
	run(t, work, "git", "init")
	// "git init -b" needs git >= 2.28; symbolic-ref sets the initial branch on
	// any version (the test already requires v2 via requireGitV2, i.e. >= 2.18).
	run(t, work, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	run(t, work, "git", "config", "user.name", "tester")
	run(t, work, "git", "config", "user.email", "tester@test")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("hello from go-git backend e2e test\n"), 0o644))
	run(t, work, "git", "add", "README.md")
	run(t, work, "git", "commit", "-m", "initial")
	run(t, work, "git", "remote", "add", "origin", "file://"+repoDir)
	run(t, work, "git", "push", "-u", "origin", "main")

	// Serve using go-git's backend. Loader base is tmp so that /testrepo.git resolves.
	loader := transport.NewFilesystemLoader(osfs.New(tmp), false)
	srv, err := internalhttp.FromLoader(loader)
	require.NoError(t, err)
	ep, err := srv.Start()
	require.NoError(t, err)

	t.Cleanup(func() { _ = srv.Close() })

	return ep, repoName
}

// TestBackend_HTTP_E2E_ClonePullPush tests cloning, pull and push using the
// HTTP transport against go-git's own backend server (explicitly not cgi-bin).
// The repo is created/seeded/operated with the git CLI (as required).
// git CLI operations explicitly force GIT_PROTOCOL=version=2 (via runV2) so that
// this test actually exercises the v2 wire protocol on the server and would catch
// bugs like the previous "packfile after ready" negotiation issues.
// The go-git HTTP transport is exercised here via the low-level Session using the
// classic (v0/v1) format; the go-git v2 client path has its own end-to-end
// coverage in TestBackend_HTTP_V2_GoGitClient.
func TestBackend_HTTP_E2E_ClonePullPush(t *testing.T) {
	t.Parallel()

	requireGitV2(t)

	base, name := setupGoGitBackendServer(t)
	authed := fmt.Sprintf("http://u:p@%s/%s", strings.TrimPrefix(base, "http://"), name)
	pu, err := url.Parse(authed)
	require.NoError(t, err)

	// --- git CLI clone (exercises v2 ls-refs + fetch against our UploadPack v2 path) ---
	cloneCLI := t.TempDir()
	runV2(t, cloneCLI, "git", "clone", authed, "cloned")
	// Force a main branch for subsequent ops (git may default to master in the worktree).
	run(t, filepath.Join(cloneCLI, "cloned"), "git", "branch", "-M", "main")

	// Verify objects arrived (pack present after v2 fetch).
	packGlob := filepath.Join(cloneCLI, "cloned", ".git", "objects", "pack", "pack-*.pack")
	matches, _ := filepath.Glob(packGlob)
	require.NotEmpty(t, matches, "v2 fetch via git CLI should have produced a pack")

	// --- git CLI modify + push (exercises receive-pack path on our server) ---
	workDir := filepath.Join(cloneCLI, "cloned")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "from-cli.txt"), []byte("from cli over http\n"), 0o644))
	run(t, workDir, "git", "add", "from-cli.txt")
	run(t, workDir, "git", "commit", "-m", "cli push test")
	// Force auth header (git can be picky with userinfo on POST to custom backends).
	runV2(t, workDir, "git", "-c", "http.extraHeader=Authorization: Basic dTpw", "push", "--force", authed, "HEAD:main")

	// --- go-git HTTP transport low-level fetch (directly tests the http transport + our server) ---
	// Note: we do not set Protocol: V2 here because the go-git client does not yet
	// implement the v2 send path (command=fetch etc.). These low-level calls use
	// the classic format (no Git-Protocol header -> v0 on server). The v2 server
	// implementation is exercised (and would fail the test if broken) by the
	// forced-v2 git CLI calls above and below.
	tr := NewTransport(Options{})
	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     pu,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer sess.Close()

	refs, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, refs.References)

	var want plumbing.Hash
	for _, r := range refs.References {
		if r.Name() == "refs/heads/main" {
			want = r.Hash()
			break
		}
	}
	require.False(t, want.IsZero())

	st := memory.NewStorage()
	require.NoError(t, sess.Fetch(context.Background(), st, &transport.FetchRequest{Wants: []plumbing.Hash{want}}))

	// Verify by decoding the tip commit/tree (Fetch populates objects; no ref update at this layer).
	cobj, err := st.EncodedObject(t.Context(), plumbing.CommitObject, want)
	require.NoError(t, err)
	c, err := object.DecodeCommit(st, cobj)
	require.NoError(t, err)
	tobj, err := st.EncodedObject(t.Context(), plumbing.TreeObject, c.TreeHash)
	require.NoError(t, err)
	trr, err := object.DecodeTree(st, tobj)
	require.NoError(t, err)
	found := false
	for _, e := range trr.Entries {
		if e.Name == "README.md" || e.Name == "from-cli.txt" {
			found = true
			break
		}
	}
	require.True(t, found, "go-git http transport fetch should have delivered objects from server")

	// --- go-git HTTP transport fetch after the CLI push (exercises "pull" using the http transport) ---
	sess3, err := tr.Handshake(context.Background(), &transport.Request{URL: pu, Command: transport.UploadPackService})
	require.NoError(t, err)
	refs3, err := sess3.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)
	var tip3 plumbing.Hash
	for _, r := range refs3.References {
		if r.Name() == "refs/heads/main" {
			tip3 = r.Hash()
			break
		}
	}
	require.False(t, tip3.IsZero())
	st3 := memory.NewStorage()
	require.NoError(t, sess3.Fetch(context.Background(), st3, &transport.FetchRequest{Wants: []plumbing.Hash{tip3}}))
	sess3.Close()

	// Confirm the blob added by the CLI push is reachable in the newly fetched objects.
	cobj3, err := st3.EncodedObject(t.Context(), plumbing.CommitObject, tip3)
	require.NoError(t, err)
	c3, err := object.DecodeCommit(st3, cobj3)
	require.NoError(t, err)
	tobj3, err := st3.EncodedObject(t.Context(), plumbing.TreeObject, c3.TreeHash)
	require.NoError(t, err)
	t3, err := object.DecodeTree(st3, tobj3)
	require.NoError(t, err)
	foundCLI := false
	for _, e := range t3.Entries {
		if e.Name == "from-cli.txt" {
			foundCLI = true
			break
		}
	}
	require.True(t, foundCLI, "go-git http transport fetch after push should see the object added by CLI push")

	// Also do a git CLI pull (using the http transport under the covers) to keep the workdir in sync.
	// This pull has local history (from clone + prior ops), so the client will send haves,
	// exercising the v2 acks/ready + delim + packfile path that was previously buggy.
	runV2(t, workDir, "git", "-c", "http.extraHeader=Authorization: Basic dTpw", "pull", "--ff-only", authed, "main")

	// Success: git CLI (v2 ls-refs/fetch + receive-pack) + go-git HTTP transport (fetch for clone + post-push pull)
	// against a pure go-git backend server (no cgi).
}

// gitOut runs a git command and returns its trimmed combined output, failing on error.
func gitOut(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v in %s: %s", args, dir, out)
	return strings.TrimSpace(string(out))
}

// TestBackend_HTTP_E2E_ShallowClone verifies that a real git CLI "clone --depth 1"
// over protocol v2 against the go-git backend produces a properly shallow clone:
// the server's shallow-info section must be framed correctly (before the packfile)
// and the client must end up grafted at the tip, even though the server's packfile
// is not yet truncated to the boundary.
func TestBackend_HTTP_E2E_ShallowClone(t *testing.T) {
	t.Parallel()

	requireGitV2(t)

	tmp := t.TempDir()
	repoName := "shallowrepo.git"
	repoDir := filepath.Join(tmp, repoName)
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	run(t, tmp, "git", "init", "--bare", repoName)
	run(t, repoDir, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	run(t, repoDir, "git", "config", "http.uploadpack", "true")

	work := filepath.Join(tmp, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))
	run(t, work, "git", "init")
	// "git init -b" needs git >= 2.28; symbolic-ref sets the initial branch on
	// any version (the test already requires v2 via requireGitV2, i.e. >= 2.18).
	run(t, work, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	run(t, work, "git", "config", "user.name", "tester")
	run(t, work, "git", "config", "user.email", "tester@test")
	for i := 1; i <= 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(work, "f.txt"), fmt.Appendf(nil, "commit %d\n", i), 0o644))
		run(t, work, "git", "add", "f.txt")
		run(t, work, "git", "commit", "-m", fmt.Sprintf("c%d", i))
	}
	run(t, work, "git", "remote", "add", "origin", "file://"+repoDir)
	run(t, work, "git", "push", "-u", "origin", "main")

	loader := transport.NewFilesystemLoader(osfs.New(tmp), false)
	srv, err := internalhttp.FromLoader(loader)
	require.NoError(t, err)
	ep, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	authed := fmt.Sprintf("http://u:p@%s/%s", strings.TrimPrefix(ep, "http://"), repoName)

	cloneDir := t.TempDir()
	runV2(t, cloneDir, "git", "clone", "--depth", "1", authed, "shallow")
	cloned := filepath.Join(cloneDir, "shallow")

	// The clone is grafted shallow: rev-list stops at the tip (depth 1), even
	// though the unbounded packfile carried the full history's objects.
	require.FileExists(t, filepath.Join(cloned, ".git", "shallow"))
	require.Equal(t, "1", gitOut(t, cloned, "rev-list", "--count", "HEAD"),
		"a --depth 1 clone must graft the tip as shallow")

	// Bounding: the parent commit's objects were never transferred (the pack is
	// truncated to the boundary, not merely grafted).
	parent := gitOut(t, work, "rev-parse", "HEAD~1")
	catFile := exec.Command("git", "cat-file", "-e", parent)
	catFile.Dir = cloned
	require.Error(t, catFile.Run(), "parent commit %s must be absent from a bounded --depth 1 clone", parent)
}
