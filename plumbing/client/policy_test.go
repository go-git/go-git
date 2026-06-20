package client

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func boolPtr(v bool) *bool { return &v }

// countingTransport records how many times Handshake was invoked so
// tests can confirm the policy gate fires before transport dispatch.
type countingTransport struct {
	calls atomic.Int32
}

func (t *countingTransport) Handshake(_ context.Context, _ *transport.Request) (transport.Session, error) {
	t.calls.Add(1)
	return nil, nil
}

func newFileRequest(t *testing.T) *transport.Request {
	t.Helper()
	u, err := transport.ParseURL("file:///tmp/repo")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &transport.Request{URL: u}
}

func TestHandshake_GatesNonUserFileByDefault(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	tr := &countingTransport{}
	c := New(WithTransport("file", tr))
	defer c.Close()

	req := newFileRequest(t)
	req.FromUser = boolPtr(false)

	_, err := c.Handshake(context.Background(), req)
	if !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("Handshake err = %v, want ErrProtocolNotAllowed", err)
	}
	if got := tr.calls.Load(); got != 0 {
		t.Fatalf("transport.Handshake called %d times; want 0", got)
	}
}

func TestHandshake_AllowsUserInitiatedFile(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	tr := &countingTransport{}
	c := New(WithTransport("file", tr))
	defer c.Close()

	req := newFileRequest(t)
	req.FromUser = boolPtr(true)

	if _, err := c.Handshake(context.Background(), req); err != nil {
		t.Fatalf("Handshake err = %v, want nil", err)
	}
	if got := tr.calls.Load(); got != 1 {
		t.Fatalf("transport.Handshake called %d times; want 1", got)
	}
}

func TestHandshake_WithProtocolPolicyPopulatesRequest(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"file": config.ProtocolAlways}

	tr := &countingTransport{}
	c := New(
		WithTransport("file", tr),
		WithProtocolPolicy(cfg),
	)
	defer c.Close()

	req := newFileRequest(t)
	req.FromUser = boolPtr(false)

	if _, err := c.Handshake(context.Background(), req); err != nil {
		t.Fatalf("Handshake err = %v, want nil (file=always)", err)
	}
	if got := tr.calls.Load(); got != 1 {
		t.Fatalf("transport.Handshake called %d times; want 1", got)
	}
}

func TestHandshake_WithFromUserDefault(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	tr := &countingTransport{}
	c := New(
		WithTransport("file", tr),
		WithUserInitiated(false),
	)
	defer c.Close()

	req := newFileRequest(t)
	// Request.FromUser left nil; client option must supply the default.

	_, err := c.Handshake(context.Background(), req)
	if !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("Handshake err = %v, want ErrProtocolNotAllowed", err)
	}
}

func TestHandshake_RequestFieldsOverrideClientDefaults(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	tr := &countingTransport{}
	c := New(
		WithTransport("file", tr),
		WithUserInitiated(false),
	)
	defer c.Close()

	req := newFileRequest(t)
	req.FromUser = boolPtr(true)

	if _, err := c.Handshake(context.Background(), req); err != nil {
		t.Fatalf("Handshake err = %v, want nil (explicit FromUser=true)", err)
	}
}

func TestConnect_AppliesProtocolPolicy(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	c := New()
	defer c.Close()

	req := newFileRequest(t)
	req.FromUser = boolPtr(false)

	_, err := c.Connect(context.Background(), req)
	if !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("Connect err = %v, want ErrProtocolNotAllowed", err)
	}
}

// Client must not leak its option defaults into the caller's
// Request — a caller reusing the same Request across calls would
// otherwise carry state across.
func TestHandshake_DoesNotMutateCallerRequest(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	cfg := config.NewConfig()
	cfg.Protocol.AllowByName = map[string]string{"file": config.ProtocolAlways}

	tr := &countingTransport{}
	c := New(
		WithTransport("file", tr),
		WithProtocolPolicy(cfg),
		WithUserInitiated(false),
	)
	defer c.Close()

	req := newFileRequest(t)
	_, err := c.Handshake(context.Background(), req)
	require.NoError(t, err)

	require.Nil(t, req.Config, "client must not write to req.Config")
	require.Nil(t, req.FromUser,
		"client must not write to req.FromUser")
}
