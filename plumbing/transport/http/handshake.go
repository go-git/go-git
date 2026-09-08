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
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// wrapDropped annotates err with the origin crossing that withheld credentials,
// when there was a crossing, a credential to withhold, and err is an
// authentication failure. The error keeps its type and message.
//
// The credential has to have existed: a caller who configured none and is
// challenged after a redirect would otherwise be told a credential of theirs
// was not sent, naming something they never had.
//
// The origins are copied out of the record, which outlives this call — it is
// stored on the session and read again for every later request.
func wrapDropped(rec *redirectRecord, err error) error {
	if err == nil {
		return err
	}
	if !rec.withheld() {
		return err
	}
	from, to, ok := rec.origins()
	if !ok {
		return err
	}
	if !errors.Is(err, transport.ErrAuthenticationRequired) &&
		!errors.Is(err, transport.ErrAuthorizationFailed) {
		return err
	}
	// originOf again on values that are already origins: it is what makes the
	// copies, so a caller mutating the error cannot reach the session's record.
	return fmt.Errorf("%w: %w", err, &transport.CredentialsDroppedError{
		From: originOf(from),
		To:   originOf(to),
	})
}

// sessionBase is the state every session carries, in one value. Both session
// types embed it, so a field added here reaches both without a signature to
// thread it through.
type sessionBase struct {
	client     *http.Client
	baseURL    *url.URL
	service    string
	authorizer Authorizer
	dropped    *redirectRecord
}

// Handshake implements transport.Transport. GETs /info/refs to discover
// refs and detects smart vs dumb HTTP.
func (t *Transport) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	service := req.Command
	// The caller's URL with its path in the spelling the requests will carry;
	// everything downstream compares against this base. See effectiveBase.
	baseURL, err := effectiveBase(req.URL)
	if err != nil {
		return nil, err
	}
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

	// Only the discovery GET carries the initial-request marker, so only it may
	// follow redirects under the default policy.
	rec := &redirectRecord{}
	httpReq, err := d.request(withRedirectRecord(withInitialRequest(ctx), rec), baseURL)
	if err != nil {
		return nil, err
	}
	// One authorizer for every credential this handshake holds — the repository
	// URL's userinfo and whatever the caller supplies for the origin it named —
	// so there is one thing to withhold rather than two that could disagree.
	cred0, err := t.acquire(ctx, baseURL, baseURL, false)
	if err != nil {
		return nil, fmt.Errorf("http transport: %w", err)
	}
	var configured Authorizer
	if cred0 != nil {
		configured = cred0.credential.Authorizer
	}
	authorizer := combine(basicAuth(baseURL.User), configured)
	// Recorded before the request goes out, and read back only on an
	// authentication failure. See redirectRecord.held.
	rec.holdsCredential(authorizer != nil)
	if err := applyAuth(httpReq, authorizer); err != nil {
		return nil, fmt.Errorf("http transport: authorize: %w", err)
	}

	client := t.resolveClient()
	resp, err := doRequest(client, httpReq)

	// Retry once at the origin a redirect reached, if it challenged. See
	// reauthenticate.
	reacq, resp, err := t.reauthenticate(ctx, client, baseURL, d, resp, err)
	if err != nil {
		// doRequest returns a non-nil response with its error for any non-2xx,
		// and checkError has already read what it needs of the body.
		if resp != nil {
			_ = resp.Body.Close()
		}
		// A credential was minted for the origin this failed at, so the
		// caller's own credential being withheld is not what went wrong.
		if reacq != nil {
			return nil, fmt.Errorf("http transport: %w", err)
		}
		return nil, fmt.Errorf("http transport: %w", wrapDropped(rec, err))
	}

	redirectedURL, err := applyRedirect(resp, baseURL)
	if err != nil {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http transport: %w", wrapDropped(rec, err))
	}
	// Copy before clearing: applyRedirect returns baseURL itself when the
	// redirect changed nothing, and baseURL belongs to the caller. Cleared
	// unconditionally, so a credential reaches the wire only through the
	// authorizer below and not by a second route no rule governs.
	cleared := *redirectedURL
	cleared.User = nil
	sessURL := &cleared

	// Re-acquire after an origin or path move, so the session's one authorizer
	// is not reused for a target the server rather than the caller chose. The
	// record preserves a sticky crossing the endpoints alone would not show;
	// paths are compared escaped, for the reason applyRedirect gives.
	if rec.crossed() || redirectedURL.EscapedPath() != baseURL.EscapedPath() {
		// Both halves of the hop-0 credential are re-derived below, each under
		// the relation deciding whether it may travel to where the chain ended
		// up; anything not re-derived stays gone, as in canonical git's
		// credential_from_url().

		// The credential the retry already spent, when there was one:
		// reauthenticate derives it from both sources under these same relations
		// against this same target, so reuse it rather than asking the caller a
		// question they have answered.
		settled := reacq
		if settled == nil {
			// Nothing was spent, so derive both halves here, under the same
			// relations reauthenticate uses.
			var fromURL Authorizer
			if credentialsMayFollow(baseURL, redirectedURL) {
				fromURL = basicAuth(baseURL.User)
			}

			cred, aerr := t.acquire(ctx, redirectedURL, baseURL, true)
			if aerr != nil {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("http transport: %w", aerr)
			}
			var fromHook Authorizer
			if cred != nil {
				fromHook = cred.credential.Authorizer
			}

			// Hop 0's order, as in reauthenticate.
			settled = &originCredential{
				origin:     originOf(redirectedURL),
				credential: &Credential{Authorizer: combine(fromURL, fromHook)},
			}
		}

		// Defense in depth: retain the settled credential only at the origin it
		// was acquired for. Unreachable while the retry cannot be redirected,
		// which is the other half of the pair — see errRetryRedirected.
		authorizer = nil
		if credentialsMayFollow(settled.origin, redirectedURL) {
			authorizer = settled.credential.Authorizer
		}
	}
	// No gate on the other path: nothing moved, so every hop satisfied the same
	// comparison and the credential is already where it is allowed to be.

	// The record annotates the session's later authentication failures with the
	// crossing, and is only an explanation while the session has no credential:
	// one minted for its own origin had nothing withheld on the way there, so
	// naming the crossing would tell the caller to supply what they already
	// supplied.
	dropped := rec
	if authorizer != nil {
		dropped = nil
	}

	base := sessionBase{
		client:     client,
		baseURL:    sessURL,
		service:    req.Command,
		authorizer: authorizer,
		dropped:    dropped,
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
	// Command consumes the whole response (it never streams the body out), so
	// release it on every path. A bare return on a decode error would otherwise
	// leak the response body and its connection. Releasing it includes the
	// discard: a decoder stops at the response's flush-pkt, and the request
	// that reuses the connection — the fetch POST after an ls-refs — follows
	// immediately.
	defer func() {
		if r.resp != nil {
			drainAndClose(r.resp.Body)
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
		if ioutil.ReadFinished(ctx, err) {
			neg.closeResponse()
		}
		return err
	}
	if neg.current == nil || neg.current.resp == nil {
		neg.current = &httpRequester{session: s, ctx: ctx}
		if err := neg.current.doPost(); err != nil {
			return err
		}
	}
	err = transport.FetchPack(ctx, st, s.caps, io.NopCloser(neg), shallows, req)
	if ioutil.ReadFinished(ctx, err) {
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
	if ioutil.ReadFinished(ctx, err) && rwc.resp != nil {
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
		if r.resp != nil {
			_ = r.resp.Body.Close()
		}
		return fmt.Errorf("http transport: %w", wrapDropped(r.session.dropped, err))
	}
	// doRequest has already turned any non-2xx into an error, so this catches
	// only a 2xx that is not 200 — one the pack protocol cannot parse.
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
		// The previous round is complete, and this round is the request that
		// reuses its connection.
		drainAndClose(n.current.resp.Body)
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

// closeResponse closes the current round's response body, without discarding
// what is left of it: Fetch calls this when it is finished with the negotiator
// altogether, so no request follows that the connection could serve.
//
// Whether the body was read to its end is the caller's affair. So is whether
// closing is safe at all — ioutil.ReadFinished answers that, and a caller that
// does not ask races the context reader wrapped around this body.
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
	return fmt.Errorf("dumb HTTP does not support push: %w", transport.ErrCommandUnsupported)
}

func (s *dumbPackSession) Close() error { return nil }

var (
	_ transport.Session   = (*smartPackSession)(nil)
	_ transport.Session   = (*dumbPackSession)(nil)
	_ transport.Transport = (*Transport)(nil)
)
