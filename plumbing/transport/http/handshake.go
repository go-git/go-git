package http

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	internal "github.com/go-git/go-git/v6/internal/transport"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// sessionBase is the state every session carries, in one value. Both session
// types embed it, so a field added here reaches both without a signature to
// thread it through.
type sessionBase struct {
	client     *http.Client
	baseURL    *url.URL
	service    string
	authorizer func(*http.Request) error
}

// Handshake implements transport.Transport. GETs /info/refs to discover
// refs and detects smart vs dumb HTTP.
func (t *Transport) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	service := req.Command
	baseURL := req.URL
	forceDumb := t.opts.ForceDumb

	// git archive over HTTP discovers protocol support through the upload-pack
	// info/refs endpoint and requires Protocol v2 (remote-curl.c). The archive
	// request itself is later POSTed to the git-upload-archive endpoint.
	discoverService := service
	discoverProtocol := req.Protocol
	if service == transport.UploadArchiveService {
		discoverService = transport.UploadPackService
		discoverProtocol = protocol.V2
	}

	d := discovery{service: discoverService, protocol: discoverProtocol, forceDumb: forceDumb}

	// Mark this as the initial request so checkRedirect allows the HTTP client
	// to follow redirects for this discovery request. Subsequent requests
	// (pack POSTs, object GETs) use a plain context and will not follow
	// redirects.
	httpReq, err := d.request(withInitialRequest(ctx), baseURL)
	if err != nil {
		return nil, err
	}
	// One authorizer for every credential this handshake holds: the repository
	// URL's userinfo and the caller's callback. The session carries the same
	// value, so there is one thing to withhold when a redirect leaves the origin
	// rather than two that could disagree.
	authorizer := combine(basicAuth(baseURL.User), t.opts.Authorizer)
	if err := applyAuth(httpReq, authorizer); err != nil {
		return nil, fmt.Errorf("http transport: authorize: %w", err)
	}

	client := t.resolveClient()
	resp, err := doRequest(client, httpReq)
	if err != nil {
		// doRequest returns a non-nil response alongside its error for any
		// non-2xx status, and checkError has already read what it needs of the
		// body. Close it, or every failed discovery leaks a body and a
		// connection.
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf("http transport: %w", err)
	}

	if err := checkError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	// Update base URL from the final redirect target.
	redirectedURL, err := applyRedirect(resp, baseURL)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	sessURL := redirectedURL

	// Clear credentials when the redirect left the origin they were issued
	// for. The session carries one authorizer, applied to every subsequent
	// request, so without this the original origin's credentials would be sent
	// to the new one.
	//
	// This uses the same predicate as stripCredentials, so the discovery GET
	// and the session that follows it agree on what an origin is. In canonical
	// git, credential_from_url() re-derives credentials from the new URL,
	// effectively wiping the old ones.
	//
	// The two are deliberately asymmetric in one respect: stripCredentials is
	// sticky over the whole chain, so an origin -> evil -> origin redirect
	// leaves the discovery GET's later hops unauthenticated even though the
	// chain returned home. This check instead compares baseURL only against
	// the final redirectedURL, so the same round trip leaves the session
	// authenticated. That is not a leak — redirectedURL's origin is the
	// original one — but it means such a chain can make the discovery GET
	// anonymous while the session's POSTs are authenticated, which can surface
	// as a confusing 401 rather than a credential exposure.
	if !credentialsMayFollow(baseURL, redirectedURL) {
		// Copy before clearing rather than writing through redirectedURL:
		// applyRedirect returns baseURL itself when the redirect changed
		// nothing, and baseURL belongs to the caller. That aliasing cannot
		// currently reach this branch — an aliased URL is trivially the same
		// origin as itself — so this keeps the two functions independent
		// rather than fixing a live bug: neither can make the other unsafe
		// by changing later.
		cleared := *redirectedURL
		cleared.User = nil
		sessURL = &cleared
		authorizer = nil
	}

	base := sessionBase{
		client:     client,
		baseURL:    sessURL,
		service:    req.Command,
		authorizer: authorizer,
	}
	return finishHandshake(resp, base, d)
}

// finishHandshake picks the smart or dumb session for a discovery response that
// has already been validated and had its credentials settled, keeping that
// dispatch out of the redirect and credential handling above it.
func finishHandshake(resp *http.Response, base sessionBase, d discovery) (transport.Session, error) {
	if d.forceDumb {
		return handshakeDumb(resp, base)
	}

	expected := fmt.Sprintf("application/x-%s-advertisement", d.service)
	if resp.Header.Get("Content-Type") == expected {
		return handshakeSmart(resp, base, d)
	}
	return handshakeDumb(resp, base)
}

func handshakeSmart(resp *http.Response, base sessionBase, d discovery) (transport.Session, error) {
	defer resp.Body.Close() //nolint:errcheck
	rd := bufio.NewReader(resp.Body)

	_, prefix, err := pktline.PeekLine(rd)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(prefix, []byte("# service=")) {
		var reply packp.SmartReply
		if err := reply.Decode(rd); err != nil {
			return nil, err
		}
		if reply.Service != d.service {
			return nil, fmt.Errorf("unexpected service name: %w", transport.ErrInvalidResponse)
		}
	}

	ver, err := transport.DiscoverVersion(rd)
	if err != nil {
		return nil, err
	}

	// git archive over HTTP is only available when the server speaks v2.
	if base.service == transport.UploadArchiveService && ver != protocol.V2 {
		return nil, transport.ErrArchiveUnsupported
	}

	if ver == protocol.V2 {
		// Protocol v2: the server sends a capability advertisement instead of
		// the v0/v1 ref advertisement. References are retrieved lazily via the
		// ls-refs command, so refs stays nil here.
		adv := &packp.CapabilityAdv{}
		if err := adv.Decode(rd); err != nil {
			return nil, err
		}
		// Protocol v2 fetch accepts "want <oid>" without the server
		// advertising allow-*-sha1-in-want, so surface the gate as
		// satisfied for exact-SHA1 refspecs (isSupportedRefSpec). The
		// v2 client only sends agent/object-format on the wire, so these
		// never leak into the request (internal.ClientCapabilities).
		adv.Capabilities.Set(capability.AllowReachableSHA1InWant)
		adv.Capabilities.Set(capability.AllowTipSHA1InWant)
		return &smartPackSession{
			sessionBase: base,
			version:     ver,
			caps:        adv.Capabilities,
		}, nil
	}

	ar := &packp.AdvRefs{}
	if err := ar.Decode(rd); err != nil && !errors.Is(err, packp.ErrEmptyAdvRefs) {
		return nil, err
	}

	if err := capability.Validate(&ar.Capabilities); err != nil {
		return nil, err
	}

	// Take the version from DiscoverVersion rather than AdvRefs.Decode's
	// independent parse of the same line, so there is one source of truth.
	ar.Version = ver

	return &smartPackSession{
		sessionBase: base,
		version:     ver,
		caps:        ar.Capabilities,
		refs:        ar,
	}, nil
}

func handshakeDumb(resp *http.Response, base sessionBase) (transport.Session, error) {
	defer resp.Body.Close() //nolint:errcheck
	rd := bufio.NewReader(resp.Body)

	var infoRefs packp.InfoRefs
	if err := infoRefs.Decode(rd); err != nil {
		return nil, err
	}

	ar := &packp.AdvRefs{}
	ar.References = infoRefs.References

	return &dumbPackSession{sessionBase: base, refs: ar}, nil
}

var (
	_ transport.Session   = (*smartPackSession)(nil)
	_ transport.Commander = (*smartPackSession)(nil)
	_ transport.Archiver  = (*smartPackSession)(nil)
)

type smartPackSession struct {
	sessionBase
	version protocol.Version
	caps    capability.List
	refs    *packp.AdvRefs
}

func (s *smartPackSession) Capabilities() *capability.List { return &s.caps }

func (s *smartPackSession) GetRemoteRefs(ctx context.Context, opts *transport.GetRemoteRefsOptions) (*transport.RemoteRefs, error) {
	forPush := s.service == transport.ReceivePackService
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
			return nil, transport.ErrEmptyRemoteRepository
		}
		return transport.NewRemoteRefs(refs), nil
	}

	if s.refs == nil {
		return nil, transport.ErrEmptyRemoteRepository
	}
	if !forPush && s.refs.IsEmpty() {
		return nil, transport.ErrEmptyRemoteRepository
	}
	refs, err := s.refs.ResolvedReferences()
	if err != nil {
		return nil, err
	}
	return transport.NewRemoteRefs(refs), nil
}

// Command implements transport.Commander. It runs a Protocol v2 command as a
// single stateless HTTP POST: the request envelope is buffered and sent, and
// the response is decoded from the response body. Fetch uses its own round
// instead so it can stream the packfile from the body; Command is for
// non-streaming commands such as ls-refs.
func (s *smartPackSession) Command(ctx context.Context, cmd string, req packp.CommandArgs, resp packp.Decoder) error {
	if s.version != protocol.V2 {
		return transport.ErrUnsupportedVersion
	}

	r := &httpRequester{session: s, ctx: ctx}
	cr := &packp.CommandRequest{
		Command:      cmd,
		Capabilities: internal.ClientCapabilities(s.caps),
		Args:         req,
	}
	if err := cr.Encode(r); err != nil {
		return err
	}
	// Command never streams the body out, so drain and close on every path; a
	// bare return on a decode error would leak the body and its connection.
	defer func() {
		if r.resp != nil {
			_, _ = io.Copy(io.Discard, r.resp.Body)
			_ = r.resp.Body.Close()
		}
	}()
	if resp != nil {
		if err := resp.Decode(r); err != nil {
			return err
		}
	}
	return nil
}

func (s *smartPackSession) Fetch(ctx context.Context, st storage.Storer, req *transport.FetchRequest) error {
	if s.version == protocol.V2 {
		return s.fetchV2(ctx, st, req)
	}

	neg := &httpNegotiator{session: s, ctx: ctx}

	shallows, err := transport.NegotiatePack(ctx, st, s.caps, true, neg, neg, req)
	if err != nil {
		// Don't close the response body here — context-wrapper goroutines
		// inside NegotiatePack may still be reading from it.
		return err
	}
	if neg.current == nil || neg.current.resp == nil {
		neg.current = &httpRequester{session: s, ctx: ctx}
		if err := neg.current.doPost(); err != nil {
			return err
		}
	}
	err = transport.FetchPack(ctx, st, s.caps, io.NopCloser(neg), shallows, req)
	// Close the response unless the read itself was a cancellation. Only then is
	// there a race: a ctxReader goroutine inside FetchPack can still be blocked
	// in the underlying Read after the <-ctx.Done() branch, and the request
	// context is what unblocks it, so nothing leaks. Otherwise FetchPack's last
	// Read returned through the result channel, its goroutine is quiescent, and
	// closing is both safe and necessary.
	//
	// Classified against err with errors.Is, never a fresh ctx.Err(): ctx can
	// turn Err() non-nil an instant after FetchPack returned quiescent, and
	// re-checking there would skip the close and leak the response.
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		neg.closeResponse()
	}
	return err
}

// fetchV2 fetches over Protocol v2. Each negotiation round is a fresh stateless
// POST; internal.FetchV2 decodes the metadata via FetchOutput and, once the
// server commits to a packfile, streams it from that round's response body.
func (s *smartPackSession) fetchV2(ctx context.Context, st storage.Storer, req *transport.FetchRequest) error {
	if req.Filter != "" && !internal.FetchSupports(s.caps, "filter") {
		return transport.ErrFilterNotSupported
	}
	if req.Depth > 0 && !internal.FetchSupports(s.caps, "shallow") {
		return transport.ErrShallowNotSupported
	}
	if err := transport.ReconcileObjectFormatV2(st, s.caps); err != nil {
		return err
	}

	round := func(args *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		r := &httpRequester{session: s, ctx: ctx}
		cr := &packp.CommandRequest{
			Command:      "fetch",
			Capabilities: internal.ClientCapabilities(s.caps),
			Args:         args,
		}
		if err := cr.Encode(r); err != nil {
			return nil, nil, err
		}
		out := &packp.FetchOutput{}
		if err := out.Decode(r); err != nil {
			// The success path hands r.resp.Body to the caller to stream; on a
			// decode error nothing downstream will, so release it here.
			if r.resp != nil {
				_ = r.resp.Body.Close()
			}
			return nil, nil, err
		}
		if r.resp == nil {
			return nil, nil, fmt.Errorf("http transport: fetch command produced no response")
		}
		// The response body is positioned at the packfile (when out.Packfile);
		// internal.FetchV2 streams it and closes the body via io.Closer.
		return out, r.resp.Body, nil
	}

	return internal.FetchV2(ctx, st, req, round)
}

func (s *smartPackSession) Push(ctx context.Context, st storage.Storer, req *transport.PushRequest) error {
	rwc := &httpRequester{session: s, ctx: ctx}
	err := transport.SendPack(ctx, st, s.caps, rwc, io.NopCloser(rwc), req)
	// Close the response unless the read itself was a cancellation: a ctxReader
	// goroutine inside SendPack can still be blocked in the underlying Read
	// after the <-ctx.Done() branch, and closing here would race it — the request
	// context tears the connection down instead. Otherwise SendPack's last Read
	// returned through the result channel and closing is both safe and necessary
	// (mirrors Fetch and internal.FetchV2's round loop).
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && rwc.resp != nil {
		_ = rwc.resp.Body.Close()
	}
	return err
}

func (s *smartPackSession) Close() error { return nil }

// Archive implements transport.Archiver. git archive over HTTP is a v2-only,
// stateless operation (remote-curl.c): the archive request is POSTed to the
// git-upload-archive endpoint and the response carries the ACK/NACK and the
// sideband-encoded archive stream.
func (s *smartPackSession) Archive(ctx context.Context, req *transport.ArchiveRequest) (io.ReadCloser, error) {
	if s.version != protocol.V2 {
		return nil, transport.ErrArchiveUnsupported
	}

	rt := &httpRequester{session: s, ctx: ctx}
	body := &httpArchiveBody{req: rt}
	archive, err := transport.Archive(ctx, rt, body, req)
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	return archive, nil
}

// httpArchiveBody adapts an httpRequester to the io.ReadCloser the archive
// client reads from: reads come from the POST response body, and Close closes
// that body. The paired httpRequester is passed to transport.Archive as the
// writer, whose Close fires the POST.
type httpArchiveBody struct{ req *httpRequester }

func (b *httpArchiveBody) Read(p []byte) (int, error) { return b.req.Read(p) }

func (b *httpArchiveBody) Close() error {
	if b.req.resp != nil {
		return b.req.resp.Body.Close()
	}
	return nil
}

// httpRequester buffers writes and fires a POST on first Read or Close.
type httpRequester struct {
	session *smartPackSession
	ctx     context.Context
	buf     bytes.Buffer
	resp    *http.Response
}

func (r *httpRequester) Write(p []byte) (int, error) { return r.buf.Write(p) }

func (r *httpRequester) Read(p []byte) (int, error) {
	if r.resp == nil {
		if err := r.doPost(); err != nil {
			return 0, err
		}
	}
	return r.resp.Body.Read(p)
}

func (r *httpRequester) Close() error {
	if r.resp == nil {
		return r.doPost()
	}
	return nil
}

func (r *httpRequester) doPost() error {
	serviceURL, err := url.JoinPath(r.session.baseURL.String(), r.session.service)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(r.ctx, http.MethodPost, serviceURL, &r.buf)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", fmt.Sprintf("application/x-%s-request", r.session.service))
	httpReq.Header.Set("Accept", fmt.Sprintf("application/x-%s-result", r.session.service))
	httpReq.Header.Set("User-Agent", capability.DefaultAgent())
	if gp := transport.GitProtocolEnv(r.session.version); gp != "" {
		httpReq.Header.Set("Git-Protocol", gp)
	}
	if err := applyAuth(httpReq, r.session.authorizer); err != nil {
		return err
	}
	r.resp, err = doRequest(r.session.client, httpReq)
	if err != nil {
		return fmt.Errorf("http transport: %w", err)
	}
	if r.resp.StatusCode != http.StatusOK {
		_ = r.resp.Body.Close()
		return fmt.Errorf("http transport: POST %s unexpected status %d", redactedURL(r.resp.Request.URL), r.resp.StatusCode)
	}
	return nil
}

// httpNegotiator supports multi-round stateless RPC negotiation by
// creating a fresh httpRequester for each round. A new round begins
// when Write is called after the previous round's response has arrived.
type httpNegotiator struct {
	session *smartPackSession
	ctx     context.Context
	current *httpRequester
}

func (n *httpNegotiator) Write(p []byte) (int, error) {
	if n.current != nil && n.current.resp != nil {
		_, _ = io.Copy(io.Discard, n.current.resp.Body)
		_ = n.current.resp.Body.Close()
		n.current = nil
	}
	if n.current == nil {
		n.current = &httpRequester{session: n.session, ctx: n.ctx}
	}
	return n.current.Write(p)
}

func (n *httpNegotiator) Read(p []byte) (int, error) {
	if n.current == nil {
		return 0, io.ErrClosedPipe
	}
	return n.current.Read(p)
}

func (n *httpNegotiator) Close() error {
	if n.current == nil {
		return nil
	}
	return n.current.Close()
}

// closeResponse closes the current HTTP response body.
// The caller (FetchPack) is expected to have already drained the body.
func (n *httpNegotiator) closeResponse() {
	if n.current != nil && n.current.resp != nil {
		_ = n.current.resp.Body.Close()
		n.current.resp = nil
	}
}

var _ transport.Session = (*dumbPackSession)(nil)

type dumbPackSession struct {
	sessionBase
	refs *packp.AdvRefs
}

func (s *dumbPackSession) Capabilities() *capability.List { return &capability.List{} }

func (s *dumbPackSession) GetRemoteRefs(_ context.Context, _ *transport.GetRemoteRefsOptions) (*transport.RemoteRefs, error) {
	if s.refs == nil {
		return nil, transport.ErrEmptyRemoteRepository
	}
	refs, err := s.refs.ResolvedReferences()
	if err != nil {
		return nil, err
	}
	return transport.NewRemoteRefs(refs), nil
}

func (s *dumbPackSession) Fetch(ctx context.Context, st storage.Storer, req *transport.FetchRequest) error {
	return s.fetchDumb(ctx, st, req)
}

func (s *dumbPackSession) Push(_ context.Context, _ storage.Storer, _ *transport.PushRequest) error {
	return fmt.Errorf("dumb HTTP does not support push")
}

func (s *dumbPackSession) Close() error { return nil }

var (
	_ transport.Session   = (*smartPackSession)(nil)
	_ transport.Session   = (*dumbPackSession)(nil)
	_ transport.Transport = (*Transport)(nil)
)
