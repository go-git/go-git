package file

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestFileTransport_Open(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{
		"/fake/repo.git": memory.NewStorage(),
	}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/fake/repo.git"},
		Command: "git-upload-pack",
	}

	sess, err := tr.Connect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.NoError(t, sess.Close())
}

func TestFileTransport_Connect(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{
		"/fake/repo.git": memory.NewStorage(),
	}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/fake/repo.git"},
		Command: "git-receive-pack",
	}

	rwc, err := tr.Connect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, rwc)
	require.NoError(t, rwc.Close())
}

func TestFileTransport_UnsupportedCommand(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{
		"/fake/repo.git": memory.NewStorage(),
	}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/fake/repo.git"},
		Command: "git-fake-command",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}

func TestFileTransport_RepoNotFound(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/nonexistent.git"},
		Command: "git-upload-pack",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
}

func TestFileTransport_ImplementsConnectable(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{})

	_, ok := any(tr).(transport.Connectable)
	assert.True(t, ok)
}

var _ transport.Conn = (*fileConn)(nil)
