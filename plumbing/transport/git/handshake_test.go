package git

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// The built-in default for git:// is ProtocolAlways, so a stock
// Request would not exercise the gate. Drive it via
// `protocol.git.allow=never` to verify the gate fires at the
// transport boundary even when a caller bypasses plumbing/client.
//
// Reference: https://github.com/git/git/blob/v2.54.0/transport.c#L1146-L1149
func TestTransport_HandshakeRespectsProtocolNever(t *testing.T) {
	t.Parallel()

	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"git": config.ProtocolNever}

	u, err := url.Parse("git://localhost/repo")
	require.NoError(t, err)

	tr := NewTransport(Options{})
	_, err = tr.Handshake(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
		Config:  cfg,
	})
	require.True(t, errors.Is(err, transport.ErrProtocolNotAllowed),
		"err=%v want ErrProtocolNotAllowed", err)
}

func TestTransport_ConnectRespectsProtocolNever(t *testing.T) {
	t.Parallel()

	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"git": config.ProtocolNever}

	u, err := url.Parse("git://localhost/repo")
	require.NoError(t, err)

	tr := NewTransport(Options{})
	_, err = tr.Connect(context.Background(), &transport.Request{
		URL:     u,
		Command: transport.UploadPackService,
		Config:  cfg,
	})
	require.True(t, errors.Is(err, transport.ErrProtocolNotAllowed),
		"err=%v want ErrProtocolNotAllowed", err)
}
