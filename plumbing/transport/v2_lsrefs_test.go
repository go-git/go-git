package transport

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func newV2Session(t testing.TB, serve func(serverConn net.Conn) error) *StreamSession {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })

	serveErr := make(chan error, 1)
	go func() { serveErr <- serve(serverConn) }()
	t.Cleanup(func() { require.NoError(t, <-serveErr) })

	conn := &testConn{
		r:     clientConn,
		w:     clientConn,
		close: func() error { return clientConn.Close() },
	}

	s, err := NewStreamSession(conn, UploadPackService)
	require.NoError(t, err)
	require.Equal(t, protocol.V2, s.version)
	return s
}

func serveUploadPackV2Once(st storage.Storer) func(net.Conn) error {
	return func(serverConn net.Conn) error {
		defer func() { _ = serverConn.Close() }()
		if err := AdvertiseCapabilities(context.TODO(), st, serverConn, UploadPackService); err != nil {
			return err
		}
		return serveUploadPackV2(
			context.TODO(),
			st,
			bufio.NewReader(serverConn),
			ioutil.WriteNopCloser(serverConn),
			&UploadPackRequest{GitProtocol: "version=2", StatelessRPC: true},
		)
	}
}

func refNames(refs []*plumbing.Reference) map[string]*plumbing.Reference {
	m := make(map[string]*plumbing.Reference, len(refs))
	for _, ref := range refs {
		m[ref.Name().String()] = ref
	}
	return m
}

func TestV2GetRemoteRefs(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	s := newV2Session(t, serveUploadPackV2Once(st))

	rr, err := s.GetRemoteRefs(context.TODO(), nil)
	require.NoError(t, err)

	byName := refNames(rr.References)
	require.Contains(t, byName, "refs/heads/master")
	require.Contains(t, byName, "HEAD")
	require.Equal(t, plumbing.SymbolicReference, byName["HEAD"].Type())
	require.Equal(t, plumbing.ReferenceName("refs/heads/master"), byName["HEAD"].Target())
	require.Empty(t, rr.Unborn)
}

func TestV2GetRemoteRefsRefPrefixes(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	s := newV2Session(t, serveUploadPackV2Once(st))

	rr, err := s.GetRemoteRefs(context.TODO(), &GetRemoteRefsOptions{
		RefPrefixes: []string{"refs/heads/"},
	})
	require.NoError(t, err)

	require.NotEmpty(t, rr.References)
	for _, ref := range rr.References {
		require.True(t, ref.Name().IsBranch(),
			"expected only refs/heads/* with the heads prefix, got %s", ref.Name())
	}
}

func TestV2GetRemoteRefsUnbornHead(t *testing.T) {
	t.Parallel()

	const unbornTarget = "refs/heads/main"
	const devHash = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	serve := func(serverConn net.Conn) (err error) {
		defer func() { _ = serverConn.Close() }()

		w := serverConn
		for _, line := range []string{
			"version 2\n",
			"agent=test\n",
			"ls-refs=unborn\n",
			"object-format=sha1\n",
		} {
			if _, err := pktline.WriteString(w, line); err != nil {
				return err
			}
		}
		if err := pktline.WriteFlush(w); err != nil {
			return err
		}

		req := &packp.CommandRequest{Args: &packp.LsRefsArgs{}}
		if err := req.Decode(bufio.NewReader(serverConn)); err != nil {
			return err
		}
		args, ok := req.Args.(*packp.LsRefsArgs)
		if !ok || !args.Unborn {
			return errors.New("server did not receive unborn request")
		}

		// An unborn HEAD alongside a real branch: HEAD points at a branch with
		// no commit yet, but the repository is not empty.
		if _, err := pktline.Writef(w, "unborn HEAD symref-target:%s\n", unbornTarget); err != nil {
			return err
		}
		if _, err := pktline.Writef(w, "%s refs/heads/dev\n", devHash); err != nil {
			return err
		}
		return pktline.WriteFlush(w)
	}

	s := newV2Session(t, serve)
	require.Contains(t, s.caps.Get(capability.LsRefs), "unborn")

	rr, err := s.GetRemoteRefs(context.TODO(), nil)
	require.NoError(t, err)
	require.Equal(t, plumbing.ReferenceName(unbornTarget), rr.Unborn)
}

func TestV2GetRemoteRefsUnbornOnlyIsEmpty(t *testing.T) {
	t.Parallel()

	serve := func(serverConn net.Conn) (err error) {
		defer func() { _ = serverConn.Close() }()

		w := serverConn
		for _, line := range []string{"version 2\n", "agent=test\n", "ls-refs=unborn\n", "object-format=sha1\n"} {
			if _, err := pktline.WriteString(w, line); err != nil {
				return err
			}
		}
		if err := pktline.WriteFlush(w); err != nil {
			return err
		}

		req := &packp.CommandRequest{Args: &packp.LsRefsArgs{}}
		if err := req.Decode(bufio.NewReader(serverConn)); err != nil {
			return err
		}

		// Only an unborn HEAD, no hash references: an empty repository.
		if _, err := pktline.WriteString(w, "unborn HEAD symref-target:refs/heads/main\n"); err != nil {
			return err
		}
		return pktline.WriteFlush(w)
	}

	s := newV2Session(t, serve)
	_, err := s.GetRemoteRefs(context.TODO(), nil)
	require.ErrorIs(t, err, ErrEmptyRemoteRepository)
}
