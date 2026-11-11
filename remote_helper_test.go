package git

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

var errRecorded = errors.New("recorded")

type recordingTransport struct {
	req *transport.Request
}

func (t *recordingTransport) Handshake(_ context.Context, req *transport.Request) (transport.Session, error) {
	t.req = req
	return nil, errRecorded
}

func TestCloneRemoteHelperURLUsesRegisteredTransport(t *testing.T) {
	t.Parallel()

	tr := &recordingTransport{}
	_, err := Clone(memory.NewStorage(), nil, &CloneOptions{
		URL:           "foo::https://example.com/repo",
		ClientOptions: []client.Option{client.WithTransport("foo", tr)},
	})

	require.ErrorIs(t, err, errRecorded)
	require.NotNil(t, tr.req)
	assert.Equal(t, "foo", tr.req.URL.Scheme)
	assert.Equal(t, "foo::https://example.com/repo", tr.req.URL.String())
}

// Without an explicit opt-in, a remote helper URL must fail rather than be
// read as the SCP-like SSH endpoint `foo:` with path `:bar`.
func TestCloneRemoteHelperURLWithoutTransport(t *testing.T) {
	t.Parallel()

	_, err := Clone(memory.NewStorage(), nil, &CloneOptions{URL: "foo::bar"})

	require.ErrorContains(t, err, `no transport registered for remote helper "foo"`)
}
