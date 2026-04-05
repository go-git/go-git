package file

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

func TestFileTransport_Integration_UploadPack(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL:      &url.URL{Scheme: "file", Path: repoPath},
		Command:  "git-upload-pack",
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
}

func TestFileTransport_Integration_ReceivePack(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL:      &url.URL{Scheme: "file", Path: repoPath},
		Command:  "git-receive-pack",
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
}

func TestFileTransport_Integration_Connect(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL:      &url.URL{Scheme: "file", Path: repoPath},
		Command:  "git-upload-pack",
		Protocol: protocol.V0,
	}

	rwc, err := tr.Connect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, rwc)

	buf := make([]byte, 4)
	n, err := rwc.Reader().Read(buf)
	require.NoError(t, err)
	assert.Greater(t, n, 0, "should read pkt-line data from server")

	require.NoError(t, rwc.Close())
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
