package transport

import (
	"bufio"
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
)

// StreamSession implements PackSession over a full-duplex stream.
// Stream transports (SSH, Git TCP, file) call NewStreamSession from
// their Handshake implementation.
type StreamSession struct {
	conn    Conn
	r       *bufio.Reader
	w       io.WriteCloser
	svc     string
	version protocol.Version
	caps    *capability.List
	refs    *packp.AdvRefs
}

// NewStreamSession reads version + adv-refs from the session and
// returns a ready StreamSession.
func NewStreamSession(conn Conn, service string) (*StreamSession, error) {
	r := bufio.NewReader(conn.Reader())
	w := conn.Writer()

	ver, err := DiscoverVersion(r)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	switch ver {
	case protocol.V2:
		_ = conn.Close()
		return nil, ErrUnsupportedVersion
	case protocol.V1, protocol.V0:
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(r); err != nil && !errors.Is(err, packp.ErrEmptyAdvRefs) {
		_ = conn.Close()
		return nil, err
	}

	return &StreamSession{
		conn:    conn,
		r:       r,
		w:       w,
		svc:     service,
		version: ver,
		caps:    ar.Capabilities,
		refs:    ar,
	}, nil
}

// Capabilities implements PackSession.
func (s *StreamSession) Capabilities() *capability.List { return s.caps }

// GetRemoteRefs implements PackSession.
func (s *StreamSession) GetRemoteRefs(_ context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, ErrEmptyRemoteRepository
	}
	forPush := s.svc == ReceivePackService
	if !forPush && s.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}
	return s.refs.MakeReferenceSlice()
}

// Fetch implements PackSession.
func (s *StreamSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	shallows, err := NegotiatePack(ctx, st, s.caps, false, s.r, s.w, req)
	if err != nil {
		return err
	}
	return FetchPack(ctx, st, s.caps, io.NopCloser(s.r), shallows, req)
}

// Push implements PackSession.
func (s *StreamSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	return SendPack(ctx, st, s.caps, s.w, io.NopCloser(s.r), req)
}

// Close implements PackSession.
func (s *StreamSession) Close() error { return s.conn.Close() }

var _ Session = (*StreamSession)(nil)
