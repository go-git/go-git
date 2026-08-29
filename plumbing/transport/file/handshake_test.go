package file

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

func boolPtr(v bool) *bool { return &v }

// Direct callers of the file transport must hit the protocol-policy
// gate without going through plumbing/client. Canonical Git puts
// the check in every *_open vtable (e.g. transport.c:1205); the Go
// equivalent puts it at the top of each transport's Handshake and
// Connect.
//
// Reference: https://github.com/git/git/blob/v2.54.0/transport.c#L1146-L1149
func TestTransport_HandshakeGatesNonUserFile(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	u, err := url.Parse("file:///tmp/repo")
	require.NoError(t, err)

	tr := NewTransport(Options{})
	_, err = tr.Handshake(context.Background(), &transport.Request{
		URL:      u,
		Command:  transport.UploadPackService,
		FromUser: boolPtr(false),
	})
	require.True(t, errors.Is(err, transport.ErrProtocolNotAllowed),
		"err=%v want ErrProtocolNotAllowed", err)
}

func TestTransport_ConnectGatesNonUserFile(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	u, err := url.Parse("file:///tmp/repo")
	require.NoError(t, err)

	tr := NewTransport(Options{})
	_, err = tr.Connect(context.Background(), &transport.Request{
		URL:      u,
		Command:  transport.UploadPackService,
		FromUser: boolPtr(false),
	})
	require.True(t, errors.Is(err, transport.ErrProtocolNotAllowed),
		"err=%v want ErrProtocolNotAllowed", err)
}
