package transport

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

const basicMasterHash = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

func TestV2FetchClone(t *testing.T) {
	t.Parallel()
	serverSt := basicV2Storage(t)
	want := plumbing.NewHash(basicMasterHash)

	clientSt := memory.NewStorage()
	s := newV2Session(t, serveUploadPackV2Once(serverSt))

	err := s.Fetch(t.Context(), clientSt, &FetchRequest{Wants: []plumbing.Hash{want}})
	require.NoError(t, err)

	obj, err := clientSt.EncodedObject(t.Context(), plumbing.CommitObject, want)
	require.NoError(t, err)
	require.Equal(t, want, obj.Hash())
}

func TestV2FetchManyRounds(t *testing.T) {
	t.Parallel()
	serverSt := basicV2Storage(t)
	want := plumbing.NewHash(basicMasterHash)

	// More haves than the initial batch (16) so negotiation needs several
	// rounds: round 1 sends 16, round 2 the remainder, round 3 sends "done".
	haves := make([]plumbing.Hash, 20)
	for i := range haves {
		haves[i] = plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d")
	}

	var rounds int
	serve := func(serverConn net.Conn) (err error) {
		defer func() { _ = serverConn.Close() }()

		w := serverConn
		for _, line := range []string{"version 2\n", "agent=test\n", "fetch=shallow\n", "object-format=sha1\n"} {
			if _, err := pktline.WriteString(w, line); err != nil {
				return err
			}
		}
		if err := pktline.WriteFlush(w); err != nil {
			return err
		}

		rd := bufio.NewReader(serverConn)
		for {
			req := &packp.CommandRequest{Args: &packp.FetchArgs{}}
			if err := req.Decode(rd); err != nil {
				return err
			}
			rounds++
			args := req.Args.(*packp.FetchArgs)
			if args.Done {
				return writeV2PackfileSection(t.Context(), w, serverSt, args.Wants, args.Haves)
			}
			if _, err := pktline.WriteString(w, "acknowledgments\n"); err != nil {
				return err
			}
			if _, err := pktline.WriteString(w, "NAK\n"); err != nil {
				return err
			}
			if err := pktline.WriteFlush(w); err != nil {
				return err
			}
		}
	}

	clientSt := memory.NewStorage()
	s := newV2Session(t, serve)

	err := s.Fetch(t.Context(), clientSt, &FetchRequest{
		Wants: []plumbing.Hash{want},
		Haves: haves,
	})
	require.NoError(t, err)
	require.Greater(t, rounds, 2, "incremental have batching should take more than two rounds")

	_, err = clientSt.EncodedObject(t.Context(), plumbing.CommitObject, want)
	require.NoError(t, err)
}

func TestV2FetchNegotiationRounds(t *testing.T) {
	t.Parallel()
	serverSt := basicV2Storage(t)
	want := plumbing.NewHash(basicMasterHash)
	have := plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d")

	var sawNegotiation bool
	serve := func(serverConn net.Conn) (err error) {
		defer func() { _ = serverConn.Close() }()

		w := serverConn
		for _, line := range []string{"version 2\n", "agent=test\n", "fetch=shallow\n", "object-format=sha1\n"} {
			if _, err := pktline.WriteString(w, line); err != nil {
				return err
			}
		}
		if err := pktline.WriteFlush(w); err != nil {
			return err
		}

		rd := bufio.NewReader(serverConn)

		first := &packp.CommandRequest{Args: &packp.FetchArgs{}}
		if err := first.Decode(rd); err != nil {
			return err
		}
		if first.Args.(*packp.FetchArgs).Done {
			return errors.New("expected negotiation round without done")
		}
		sawNegotiation = true
		if _, err := pktline.WriteString(w, "acknowledgments\n"); err != nil {
			return err
		}
		if _, err := pktline.WriteString(w, "NAK\n"); err != nil {
			return err
		}
		if err := pktline.WriteFlush(w); err != nil {
			return err
		}

		second := &packp.CommandRequest{Args: &packp.FetchArgs{}}
		if err := second.Decode(rd); err != nil {
			return err
		}
		args := second.Args.(*packp.FetchArgs)
		if !args.Done {
			return errors.New("expected done on second round")
		}
		return writeV2PackfileSection(t.Context(), w, serverSt, args.Wants, args.Haves)
	}

	clientSt := memory.NewStorage()
	s := newV2Session(t, serve)

	err := s.Fetch(t.Context(), clientSt, &FetchRequest{
		Wants: []plumbing.Hash{want},
		Haves: []plumbing.Hash{have},
	})
	require.NoError(t, err)
	require.True(t, sawNegotiation)

	_, err = clientSt.EncodedObject(t.Context(), plumbing.CommitObject, want)
	require.NoError(t, err)
}

func TestV2FetchUnsupportedFeatures(t *testing.T) {
	t.Parallel()

	// A v2 session whose server advertised no fetch features.
	s := &StreamSession{version: protocol.V2}

	err := s.Fetch(t.Context(), memory.NewStorage(), &FetchRequest{
		Wants:  []plumbing.Hash{plumbing.NewHash(basicMasterHash)},
		Filter: "blob:none",
	})
	require.ErrorIs(t, err, ErrFilterNotSupported)

	err = s.Fetch(t.Context(), memory.NewStorage(), &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash(basicMasterHash)},
		Depth: 1,
	})
	require.ErrorIs(t, err, ErrShallowNotSupported)
}

func writeV2PackfileSection(ctx context.Context, w net.Conn, st storage.Storer, wants, haves []plumbing.Hash) error {
	if _, err := pktline.WriteString(w, "packfile\n"); err != nil {
		return err
	}
	objs, err := objectsToUpload(ctx, st, wants, haves)
	if err != nil {
		return err
	}
	mux := sideband.NewMuxer(sideband.Sideband64k, w)
	e := packfile.NewEncoder(ctx, mux, st, false)
	if _, err := e.Encode(ctx, objs, 10); err != nil {
		return err
	}
	return pktline.WriteFlush(w)
}
