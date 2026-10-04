package git

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	serverhttp "github.com/go-git/go-git/v6/internal/server/http"
	"github.com/go-git/go-git/v6/internal/test/gitenv"
	transporttest "github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// requireInteropGit adds to requireGitBinary the conditions of the interop
// tests: not on Windows, where git for windows is unreliable for write
// operations, and a git that can create sha256 repositories.
func requireInteropGit(t *testing.T) {
	t.Helper()
	requireGitBinary(t)
	if runtime.GOOS == "windows" {
		t.Skip("interop: write operations through git for windows are unreliable in these tests")
	}
	// SHA-256 repositories and init -b need git 2.29+.
	probe := gitenv.Command("git", "init", "-q", "-b", "main", "--object-format=sha256", t.TempDir())
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("interop: git cannot create sha256 repositories: %v: %s", err, out)
	}
}

// interopAuthHeader satisfies go-git's HTTP backend, which requires an
// Authorization header for receive-pack.
const interopAuthHeader = "http.extraHeader=Authorization: Basic dTpw"

// withUserinfo adds credentials to an http:// endpoint for go-git's client.
func withUserinfo(endpoint string) string {
	return strings.Replace(endpoint, "http://", "http://u:p@", 1)
}

func gitRunErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitRunErr(t, dir, args...)
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(out)
}

// newBareRepo creates base/name as a bare repository in format whose HEAD
// points at main (gitenv's init.defaultBranch is master).
func newBareRepo(t *testing.T, base, name, format string) string {
	t.Helper()
	gitRun(t, base, "init", "-q", "--bare", "-b", "main", "--object-format="+format, name)
	return filepath.Join(base, name)
}

// newClientRepo creates a repository in format with one commit on main.
func newClientRepo(t *testing.T, format string) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main", "--object-format="+format, ".")
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

// startGoGitHTTP serves base over HTTP with go-git's server. HTTP rather than
// go-git's git:// server: the latter hangs on any push carrying a pack (the
// pack copy reads to EOF and a stateful client never half-closes), a
// pre-existing bug outside this change.
func startGoGitHTTP(t *testing.T, base string) string {
	t.Helper()
	srv, err := serverhttp.FromLoader(transport.NewFilesystemLoader(osfs.New(base), false))
	require.NoError(t, err)
	endpoint, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
	return strings.TrimSuffix(endpoint, "/")
}

// startNativeDaemon serves base over git:// with git daemon, receive-pack
// enabled.
func startNativeDaemon(t *testing.T, base string) string {
	t.Helper()
	port, err := transporttest.FreePort()
	require.NoError(t, err)
	transporttest.StartGitDaemon(t, base, port, "--enable=receive-pack", "--informative-errors")
	return fmt.Sprintf("git://127.0.0.1:%d", port)
}

// goGitPush pushes refspec from the repository at repoDir to url with
// go-git's client.
func goGitPush(t *testing.T, repoDir, url, refspec string) error {
	t.Helper()
	r, err := PlainOpen(repoDir)
	require.NoError(t, err)
	_, err = r.CreateRemote(&config.RemoteConfig{Name: "interop", URLs: []string{url}})
	require.NoError(t, err)
	return r.Push(&PushOptions{
		RemoteName: "interop",
		RefSpecs:   []config.RefSpec{config.RefSpec(refspec)},
	})
}

// A go-git client pushes a sha256 repository to native git.
func TestInteropGoGitClientPushSHA256ToNativeGit(t *testing.T) {
	t.Parallel()
	requireInteropGit(t)

	base := t.TempDir()
	bare := newBareRepo(t, base, "s256.git", "sha256")
	url := startNativeDaemon(t, base)
	client := newClientRepo(t, "sha256")

	require.NoError(t, goGitPush(t, client, url+"/s256.git", "refs/heads/main:refs/heads/main"))
	require.Equal(t,
		gitRun(t, client, "rev-parse", "HEAD"),
		gitRun(t, bare, "rev-parse", "refs/heads/main"))
}

// A go-git client pushes a sha256 repository to a go-git server.
func TestInteropGoGitClientPushSHA256ToGoGitServer(t *testing.T) {
	t.Parallel()
	requireInteropGit(t)

	base := t.TempDir()
	bare := newBareRepo(t, base, "s256.git", "sha256")
	url := withUserinfo(startGoGitHTTP(t, base))
	client := newClientRepo(t, "sha256")

	require.NoError(t, goGitPush(t, client, url+"/s256.git", "refs/heads/main:refs/heads/main"))
	require.Equal(t,
		gitRun(t, client, "rev-parse", "HEAD"),
		gitRun(t, bare, "rev-parse", "refs/heads/main"))
}

// Native git pushes a sha256 repository once the go-git server advertises
// object-format.
func TestInteropNativePushSHA256ToGoGitServer(t *testing.T) {
	t.Parallel()
	requireInteropGit(t)

	base := t.TempDir()
	bare := newBareRepo(t, base, "s256.git", "sha256")
	url := startGoGitHTTP(t, base)
	client := newClientRepo(t, "sha256")

	gitRun(t, client, "-c", interopAuthHeader, "push", url+"/s256.git", "HEAD:refs/heads/main")
	require.Equal(t,
		gitRun(t, client, "rev-parse", "HEAD"),
		gitRun(t, bare, "rev-parse", "refs/heads/main"))
}

// A sha1 push into a sha256 repository. Now that the server
// advertises its format, git's send-pack refuses before sending anything;
// before this change the server parsed the sha1 pack as sha256 and left a
// temporary pack behind. The server-side check itself is covered by the
// receive-pack unit tests.
func TestInteropNativePushSHA1ToGoGitServerSHA256Rejected(t *testing.T) {
	t.Parallel()
	requireInteropGit(t)

	base := t.TempDir()
	bare := newBareRepo(t, base, "s256.git", "sha256")
	url := startGoGitHTTP(t, base)
	client := newClientRepo(t, "sha1")

	out, err := gitRunErr(t, client, "-c", interopAuthHeader, "push", url+"/s256.git", "HEAD:refs/heads/main")
	require.Error(t, err, out)
	require.Contains(t, out, "the receiving end does not support this repository's hash algorithm")

	refs := gitRun(t, bare, "for-each-ref")
	require.Empty(t, refs)
	tmp, err := filepath.Glob(filepath.Join(bare, "objects", "pack", "tmp_pack_*"))
	require.NoError(t, err)
	require.Empty(t, tmp)
}

// git's v2 client sends object-format; the go-git server must keep serving
// it after the check.
func TestInteropNativeFetchV2SHA256FromGoGitServer(t *testing.T) {
	t.Parallel()
	requireInteropGit(t)

	base := t.TempDir()
	newBareRepo(t, base, "s256.git", "sha256")
	src := newClientRepo(t, "sha256")
	gitRun(t, src, "push", "-q", filepath.Join(base, "s256.git"), "HEAD:refs/heads/main")
	url := startGoGitHTTP(t, base)

	dst := t.TempDir()
	gitRun(t, dst, "-c", "protocol.version=2", "clone", "-q", url+"/s256.git", "clone")
	require.Equal(t,
		gitRun(t, src, "rev-parse", "HEAD"),
		gitRun(t, filepath.Join(dst, "clone"), "rev-parse", "HEAD"))
}
