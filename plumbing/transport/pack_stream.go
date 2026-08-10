package transport

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"

	internal "github.com/go-git/go-git/v6/internal/transport"
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

	s.version = ver

	if ver == protocol.V2 {
		// Protocol v2: the server sends a capability advertisement
		// (version line + capability lines) instead of the v0/v1 ref
		// advertisement. References are retrieved lazily via the ls-refs
		// command, so nothing is read here beyond the advertisement.
		adv := &packp.CapabilityAdv{}
		if err := adv.Decode(r); err != nil {
			_ = conn.Close()
			return nil, err
		}
		s.caps = adv.Capabilities
		// Protocol v2 fetch accepts "want <oid>" without the server
		// advertising allow-*-sha1-in-want, so surface the gate as
		// satisfied for exact-SHA1 refspecs (isSupportedRefSpec). The
		// v2 client only sends agent/object-format on the wire, so these
		// never leak into the request (internal.ClientCapabilities).
		s.caps.Set(capability.AllowReachableSHA1InWant)
		s.caps.Set(capability.AllowTipSHA1InWant)
		return s, nil
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

	// Source the advertisement's version from the version DiscoverVersion
	// already established, so s.version is the single source of truth rather
	// than relying on AdvRefs.Decode's independent parse of the same line.
	ar.Version = ver
	s.caps = ar.Capabilities
	s.refs = ar

	return s, nil
}

// Capabilities implements PackSession.
func (s *StreamSession) Capabilities() *capability.List { return &s.caps }

// GetRemoteRefs implements Session. For v0/v1 the server advertises every
// reference during the handshake, so opts is ignored. For v2 the references
// are retrieved on demand via the ls-refs command, honoring the ref-prefix
// filters in opts.
func (s *StreamSession) GetRemoteRefs(ctx context.Context, opts *GetRemoteRefsOptions) (*RemoteRefs, error) {
	forPush := s.svc == ReceivePackService
	if s.version == protocol.V2 {
		var prefixes []string
		if opts != nil {
			prefixes = opts.RefPrefixes
		}
		refs, err := internal.LsRefs(ctx, s.Command, s.caps, prefixes)
		if err != nil {
			return nil, err
		}
		if !forPush && !internal.HasHashRef(refs) {
			return nil, ErrEmptyRemoteRepository
		}
		return NewRemoteRefs(refs), nil
	}

	if s.refs == nil {
		return nil, ErrEmptyRemoteRepository
	}
	if !forPush && s.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	refs, err := s.refs.ResolvedReferences()
	if err != nil {
		return nil, err
	}
	return NewRemoteRefs(refs), nil
}

// Fetch implements PackSession.
func (s *StreamSession) Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error {
	if s.version == protocol.V2 {
		if req.Filter != "" && !internal.FetchSupports(s.caps, "filter") {
			return ErrFilterNotSupported
		}
		if req.Depth > 0 && !internal.FetchSupports(s.caps, "shallow") {
			return ErrShallowNotSupported
		}
		if err := ReconcileObjectFormatV2(st, s.caps); err != nil {
			return err
		}
		// Each negotiation round reuses the persistent stream: Command writes
		// the request and decodes the metadata, leaving s.r at the packfile.
		round := func(args *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
			out := &packp.FetchOutput{}
			if err := s.Command(ctx, "fetch", args, out); err != nil {
				return nil, nil, err
			}
			return out, s.r, nil
		}
		if err := internal.FetchV2(ctx, st, req, round); err != nil {
			return s.wrapStderr(err)
		}
		return nil
	}

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

// Command implements Commander. It builds a Protocol v2 request envelope for
// the named command, encodes it, and decodes the response. The request
// carries the capabilities collected during the handshake (the agent and the
// server's object-format), so callers only provide the command arguments.
//
// Command is only valid on a session that negotiated Protocol v2.
func (s *StreamSession) Command(ctx context.Context, cmd string, req packp.CommandArgs, resp packp.Decoder) error {
	if s.version != protocol.V2 {
		return ErrUnsupportedVersion
	}

	cr := &packp.CommandRequest{
		Command:      cmd,
		Capabilities: s.commandCapabilities(),
		Args:         req,
	}

	if err := cr.Encode(ioutil.NewContextWriter(ctx, s.w)); err != nil {
		return s.wrapStderr(err)
	}

	if resp != nil {
		if err := resp.Decode(ioutil.NewContextReader(ctx, s.r)); err != nil {
			return s.wrapStderr(err)
		}
	}

	return nil
}

// commandCapabilities returns the capabilities the client sends with each v2
// command. Both the agent and the object-format are gated on the server having
// advertised them (see internal.ClientCapabilities), so the client never sends
// a capability the server did not offer; the object-format is echoed back so
// both sides agree on the hash algorithm.
func (s *StreamSession) commandCapabilities() capability.List {
	return internal.ClientCapabilities(s.caps)
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
	_ Session   = (*StreamSession)(nil)
	_ Archiver  = (*StreamSession)(nil)
	_ Commander = (*StreamSession)(nil)
)
