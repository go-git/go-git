package transport

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// StreamSession implements Session over a full-duplex stream.
// Stream transports (SSH, Git TCP, file) call NewStreamSession from
// their Handshake implementation.
type StreamSession struct {
	conn    Conn
	r       *bufio.Reader
	w       io.WriteCloser
	svc     string
	version protocol.Version
	caps    capability.List
	refs    *packp.AdvRefs
}

// NewStreamSession creates a session from an open Conn.
// For pack services (upload-pack, receive-pack), it reads the version
// and advertised refs from the stream. For upload-archive, it skips
// that — the archive protocol has no ref advertisement.
func NewStreamSession(conn Conn, service string) (*StreamSession, error) {
	r := bufio.NewReader(conn.Reader())
	w := conn.Writer()

	s := &StreamSession{
		conn: conn,
		r:    r,
		w:    w,
		svc:  service,
	}

	if service == UploadArchiveService {
		return s, nil
	}

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

	ar := &packp.AdvRefs{}
	if err := ar.Decode(r); err != nil && !errors.Is(err, packp.ErrEmptyAdvRefs) {
		_ = conn.Close()
		return nil, err
	}

	// Validate capabilities before returning the session.
	if err := capability.Validate(&ar.Capabilities); err != nil {
		_ = conn.Close()
		return nil, err
	}

	s.version = ver
	s.caps = ar.Capabilities
	s.refs = ar

	return s, nil
}

// Capabilities implements PackSession.
func (s *StreamSession) Capabilities() *capability.List { return &s.caps }

// GetRemoteRefs implements PackSession.
func (s *StreamSession) GetRemoteRefs(_ context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, ErrEmptyRemoteRepository
	}
	forPush := s.svc == ReceivePackService
	if !forPush && s.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	refs, err := s.refs.ResolvedReferences()
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// Fetch implements PackSession.
func (s *StreamSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	shallows, err := NegotiatePack(ctx, st, s.caps, false, s.r, s.w, req)
	if err != nil {
		return s.wrapStderr(err)
	}
	if err := FetchPack(ctx, st, s.caps, io.NopCloser(s.r), shallows, req); err != nil {
		return s.wrapStderr(err)
	}
	return nil
}

// Push implements PackSession.
func (s *StreamSession) Push(ctx context.Context, st storage.Storer, req *PushRequest) error {
	if err := SendPack(ctx, st, s.caps, s.w, io.NopCloser(s.r), req); err != nil {
		return s.wrapStderr(err)
	}
	return nil
}

// wrapStderr checks if the underlying connection has stderr output and
// returns it as a RemoteError so that remote error messages surface at
// the operation site rather than at Close time.
func (s *StreamSession) wrapStderr(err error) error {
	type stderrer interface {
		Stderr() io.Reader
	}
	if se, ok := s.conn.(stderrer); ok {
		if r := se.Stderr(); r != nil {
			b, readErr := io.ReadAll(r)
			if readErr == nil && len(b) > 0 {
				return NewRemoteError(strings.TrimSpace(string(b)))
			}
		}
	}
	return err
}

// Close implements Session.
func (s *StreamSession) Close() error { return s.conn.Close() }

// Archive implements Archiver. It speaks the git-upload-archive wire
// protocol over the session's existing connection.
func (s *StreamSession) Archive(ctx context.Context, req *ArchiveRequest) (io.ReadCloser, error) {
	if s.svc != UploadArchiveService {
		return nil, ErrArchiveUnsupported
	}

	rc := ioutil.NewReadCloser(s.conn.Reader(), s.conn)
	archive, err := Archive(ctx, s.conn.Writer(), rc, req)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}

	return archive, nil
}

var (
	_ Session  = (*StreamSession)(nil)
	_ Archiver = (*StreamSession)(nil)
)
