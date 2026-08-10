package transport

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func TestStreamSessionV2Handshake(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)

	var adv bytes.Buffer
	require.NoError(t, AdvertiseCapabilities(context.TODO(), st, &adv, UploadPackService))

	conn := &testConn{
		r:     &adv,
		w:     ioutil.WriteNopCloser(io.Discard),
		close: func() error { return nil },
	}

	s, err := NewStreamSession(conn, UploadPackService)
	require.NoError(t, err)

	require.Equal(t, protocol.V2, s.version)
	require.Nil(t, s.refs)

	caps := s.Capabilities()
	require.True(t, caps.Supports(capability.LsRefs))
	require.True(t, caps.Supports(capability.FetchCmd))
	require.NotEmpty(t, caps.Get(capability.ObjectFormat))
}

func TestStreamSessionCommandEnvelope(t *testing.T) {
	t.Parallel()

	serverCaps := capability.List{}
	serverCaps.Set(capability.Agent, "git/test")
	serverCaps.Set(capability.ObjectFormat, "sha1")

	var out bytes.Buffer
	s := &StreamSession{
		version: protocol.V2,
		w:       ioutil.WriteNopCloser(&out),
		caps:    serverCaps,
	}

	req := &packp.LsRefsArgs{
		Symrefs:     true,
		RefPrefixes: []string{"refs/heads/"},
	}
	require.NoError(t, s.Command(context.TODO(), "ls-refs", req, nil))

	got := &packp.CommandRequest{Args: &packp.LsRefsArgs{}}
	require.NoError(t, got.Decode(&out))

	require.Equal(t, "ls-refs", got.Command)
	require.Equal(t, []string{capability.DefaultAgent()}, got.Capabilities.Get(capability.Agent))
	require.Equal(t, []string{"sha1"}, got.Capabilities.Get(capability.ObjectFormat))

	gotArgs, ok := got.Args.(*packp.LsRefsArgs)
	require.True(t, ok)
	require.True(t, gotArgs.Symrefs)
	require.Equal(t, []string{"refs/heads/"}, gotArgs.RefPrefixes)
}

func TestStreamSessionCommandRejectsNonV2(t *testing.T) {
	t.Parallel()

	s := &StreamSession{
		version: protocol.V1,
		w:       ioutil.WriteNopCloser(io.Discard),
	}

	err := s.Command(context.TODO(), "ls-refs", nil, nil)
	require.ErrorIs(t, err, ErrUnsupportedVersion)
}

func TestStreamSessionCommandLsRefsEndToEnd(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })

	serveErr := make(chan error, 1)
	go func() {
		if err := AdvertiseCapabilities(context.TODO(), st, serverConn, UploadPackService); err != nil {
			serveErr <- err
			return
		}
		serveErr <- serveUploadPackV2(
			context.TODO(),
			st,
			bufio.NewReader(serverConn),
			ioutil.WriteNopCloser(serverConn),
			&UploadPackRequest{GitProtocol: "version=2", StatelessRPC: true},
		)
		_ = serverConn.Close()
	}()

	conn := &testConn{
		r:     clientConn,
		w:     clientConn,
		close: func() error { return clientConn.Close() },
	}

	s, err := NewStreamSession(conn, UploadPackService)
	require.NoError(t, err)
	require.Equal(t, protocol.V2, s.version)

	req := &packp.LsRefsArgs{Symrefs: true, RefPrefixes: []string{"refs/heads/"}}
	out := &packp.LsRefsOutput{}
	require.NoError(t, s.Command(context.TODO(), "ls-refs", req, out))
	require.NoError(t, <-serveErr)

	require.NotEmpty(t, out.References)
	var foundMaster bool
	for _, ref := range out.References {
		if ref.Name().String() == "refs/heads/master" {
			foundMaster = true
		}
	}
	require.True(t, foundMaster, "expected refs/heads/master in ls-refs output")
}
