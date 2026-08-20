package rad

import (
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

// fakeRID and fakeNID are used to build the synthetic RAD_HOME fixture.
// They only need to satisfy validateID's charset, not be real base58.
const (
	fakeRID = "zFixtureRepoIdAAAAAAAAAAAAAAAAA"
	fakeNID = "zFixtureNodeIdBBBBBBBBBBBBBBBBB"
)

// runGit runs git with args, optionally in dir, and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return string(out)
}

// buildFixtureHome builds a synthetic RAD_HOME under t.TempDir(): a bare
// repository at storage/<fakeRID>/ with refs/heads/main, refs/tags/v1.0.0,
// refs/rad/sigrefs and refs/namespaces/<fakeNID>/refs/heads/main, mirroring
// the shape of real Radicle storage. Returns the home directory and the
// commit hash of refs/heads/main.
func buildFixtureHome(t *testing.T) (home string, mainCommit plumbing.Hash) {
	t.Helper()

	home = t.TempDir()
	storageDir := filepath.Join(home, "storage", fakeRID)
	require.NoError(t, os.MkdirAll(filepath.Dir(storageDir), 0o755))
	runGit(t, "", "init", "-q", "--bare", storageDir)
	// "git init -b" needs git >= 2.28; symbolic-ref sets the initial branch
	// on any version.
	runGit(t, "", "--git-dir="+storageDir, "symbolic-ref", "HEAD", "refs/heads/main")

	work := t.TempDir()
	runGit(t, work, "init", "-q")
	runGit(t, work, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, work, "config", "user.email", "rad-test@example.com")
	runGit(t, work, "config", "user.name", "rad-test")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644))
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-q", "-m", "initial commit")
	runGit(t, work, "tag", "v1.0.0")
	runGit(t, work, "remote", "add", "origin", storageDir)
	runGit(t, work, "push", "-q", "origin", "main", "v1.0.0")

	out := runGit(t, work, "rev-parse", "HEAD")
	mainCommit = plumbing.NewHash(strings.TrimSpace(out))
	require.False(t, mainCommit.IsZero())

	// refs/rad/sigrefs is Radicle's signed-refs metadata; a stand-in value
	// is enough since this transport never reads it.
	runGit(t, "", "--git-dir="+storageDir, "update-ref", "refs/rad/sigrefs", mainCommit.String())
	// The peer's own namespaced copy of refs/heads/main.
	runGit(t, "", "--git-dir="+storageDir, "update-ref",
		"refs/namespaces/"+fakeNID+"/refs/heads/main", mainCommit.String())

	return home, mainCommit
}

func radURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestTransport_ReceivePackRejected(t *testing.T) {
	t.Parallel()

	home, _ := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home})

	_, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     radURL(t, "rad://"+fakeRID),
		Command: transport.ReceivePackService,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)

	_, err = tr.Connect(context.Background(), &transport.Request{
		URL:     radURL(t, "rad://"+fakeRID),
		Command: transport.ReceivePackService,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}

func TestTransport_UploadArchiveRejected(t *testing.T) {
	t.Parallel()

	home, _ := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home})

	_, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     radURL(t, "rad://"+fakeRID),
		Command: transport.UploadArchiveService,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}

func TestTransport_UnknownRID_NamesRadSeed(t *testing.T) {
	t.Parallel()

	home, _ := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home})

	const missing = "zMissingRepoIdCCCCCCCCCCCCCCCCC"
	_, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     radURL(t, "rad://"+missing),
		Command: transport.UploadPackService,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrRepositoryNotFound)
	assert.Contains(t, err.Error(), "rad seed "+missing)
}

func TestTransport_CanonicalAdvertisement(t *testing.T) {
	t.Parallel()

	home, mainCommit := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home})

	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      radURL(t, "rad://"+fakeRID),
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	refs, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)

	var names []string
	var head bool
	var main plumbing.Hash
	for _, ref := range refs.References {
		names = append(names, ref.Name().String())
		if ref.Name() == plumbing.HEAD {
			head = true
		}
		if ref.Name() == "refs/heads/main" {
			main = ref.Hash()
		}
	}

	assert.True(t, head, "canonical view should advertise HEAD")
	assert.Equal(t, mainCommit, main)
	assert.ElementsMatch(t, []string{"HEAD", "refs/heads/main", "refs/tags/v1.0.0"}, names)
}

func TestTransport_AllRefsAdvertisement(t *testing.T) {
	t.Parallel()

	home, _ := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home, AllRefs: true})

	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      radURL(t, "rad://"+fakeRID),
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	refs, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)

	var names []string
	for _, ref := range refs.References {
		names = append(names, ref.Name().String())
	}

	assert.Contains(t, names, "refs/rad/sigrefs")
	assert.Contains(t, names, "refs/namespaces/"+fakeNID+"/refs/heads/main")
}

func TestTransport_NamespacedAdvertisement(t *testing.T) {
	t.Parallel()

	home, mainCommit := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home})

	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      radURL(t, "rad://"+fakeRID+"/"+fakeNID),
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	refs, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)

	var names []string
	var head bool
	var main plumbing.Hash
	for _, ref := range refs.References {
		names = append(names, ref.Name().String())
		if ref.Name() == plumbing.HEAD {
			head = true
		}
		if ref.Name() == "refs/heads/main" {
			main = ref.Hash()
		}
	}

	assert.False(t, head, "namespaced view must not advertise HEAD")
	assert.Equal(t, mainCommit, main)
	assert.ElementsMatch(t, []string{"refs/heads/main"}, names)
}

func TestTransport_FullClone(t *testing.T) {
	t.Parallel()

	home, mainCommit := buildFixtureHome(t)
	tr := NewTransport(Options{Home: home})

	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      radURL(t, "rad://"+fakeRID),
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	st := memory.NewStorage()
	err = sess.Fetch(context.Background(), st, &transport.FetchRequest{
		Wants: []plumbing.Hash{mainCommit},
	})
	require.NoError(t, err)

	obj, err := st.EncodedObject(plumbing.CommitObject, mainCommit)
	require.NoError(t, err)
	assert.Equal(t, mainCommit, obj.Hash())
}

func TestTransport_HomeResolution(t *testing.T) {
	// Subtests use t.Setenv, which cannot be combined with t.Parallel
	// anywhere in the chain.
	home, _ := buildFixtureHome(t)

	t.Run("RAD_HOME env var", func(t *testing.T) {
		t.Setenv("RAD_HOME", home)
		tr := NewTransport(Options{})
		sess, err := tr.Handshake(context.Background(), &transport.Request{
			URL:     radURL(t, "rad://"+fakeRID),
			Command: transport.UploadPackService,
		})
		require.NoError(t, err)
		_ = sess.Close()
	})

	t.Run("Options.Home takes precedence", func(t *testing.T) {
		t.Setenv("RAD_HOME", t.TempDir()) // deliberately wrong
		tr := NewTransport(Options{Home: home})
		sess, err := tr.Handshake(context.Background(), &transport.Request{
			URL:     radURL(t, "rad://"+fakeRID),
			Command: transport.UploadPackService,
		})
		require.NoError(t, err)
		_ = sess.Close()
	})
}

func TestLoader_AllRefsViewIsReadOnly(t *testing.T) {
	t.Parallel()

	// AllRefs skips the canonical filter, but must not hand the pack
	// protocol a writable storer: a push into Radicle storage leaves it
	// inconsistent until refs/rad/sigrefs is re-signed.
	home, _ := buildFixtureHome(t)

	st, err := newLoader(Options{Home: home, AllRefs: true}).Load(radURL(t, "rad://"+fakeRID))
	require.NoError(t, err)
	defer func() { _ = st.(io.Closer).Close() }()

	err = st.SetReference(plumbing.NewHashReference("refs/heads/pushed", plumbing.ZeroHash))
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}
