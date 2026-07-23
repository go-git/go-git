package file

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestFileTransport_Integration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		command string
	}{
		{"UploadPack", "git-upload-pack"},
		{"ReceivePack", "git-receive-pack"},
		{"Connect", "git-upload-pack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()
			repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
			repoPath, err := filepath.Abs(repoFS.Root())
			require.NoError(t, err)

			tr := NewTransport(Options{})

			req := &transport.Request{
				URL:      &url.URL{Scheme: "file", Path: repoPath},
				Command:  tc.command,
				Protocol: protocol.V0,
			}

			sess, err := tr.Connect(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, sess)

			buf := make([]byte, 4)
			n, err := sess.Reader().Read(buf)
			require.NoError(t, err)
			assert.Greater(t, n, 0, "should read pkt-line data from server")

			require.NoError(t, sess.Close())
		})
	}
}

func TestFileTransport_Integration_NonExistentRepo(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/nonexistent/repo.git"},
		Command: "git-upload-pack",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
}

// TestFileTransport_Integration_V2 exercises the full Protocol v2 client path
// over the file transport against go-git's own upload-pack server: the version
// is negotiated via GIT_PROTOCOL, references come from ls-refs, and objects are
// fetched with the v2 fetch command. The stream transports (file, git://, ssh)
// share NewStreamSession, so this also covers their v2 wiring.
func TestFileTransport_Integration_V2(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})
	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      &url.URL{Scheme: "file", Path: repoPath},
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	refs, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)

	var master plumbing.Hash
	var sawHead bool
	for _, ref := range refs.References {
		if ref.Name() == "refs/heads/master" {
			master = ref.Hash()
		}
		if ref.Name() == plumbing.HEAD {
			sawHead = true
		}
	}
	require.False(t, master.IsZero(), "v2 ls-refs should advertise refs/heads/master")
	require.True(t, sawHead, "v2 ls-refs should advertise a symbolic HEAD")

	st := memory.NewStorage()
	require.NoError(t, sess.Fetch(context.Background(), st, &transport.FetchRequest{
		Wants: []plumbing.Hash{master},
	}))

	obj, err := st.EncodedObject(t.Context(), plumbing.CommitObject, master)
	require.NoError(t, err)
	require.Equal(t, master, obj.Hash())
}
