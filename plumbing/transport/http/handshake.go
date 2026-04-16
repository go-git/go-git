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

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// Handshake implements transport.Transport. GETs /info/refs to discover
// refs and detects smart vs dumb HTTP.
func (t *Transport) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	service := req.Command
	baseURL := req.URL
	forceDumb := t.opts.ForceDumb

	infoURL, err := url.JoinPath(baseURL.String(), "info/refs")
	if err != nil {
		return nil, err
	}
	if !forceDumb {
		infoURL += "?service=" + service
	}

	// Mark this as the initial request so checkRedirect allows
	// the HTTP client to follow redirects for this discovery request.
	// Subsequent requests (pack POSTs, object GETs) use a plain
	// context and will not follow redirects.
	httpReq, err := http.NewRequestWithContext(withInitialRequest(ctx), http.MethodGet, infoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("http transport: %w", err)
	}

	httpReq.Header.Set("User-Agent", capability.DefaultAgent())
	if !forceDumb {
		if gp := transport.GitProtocolEnv(req.Protocol); gp != "" {
			httpReq.Header.Set("Git-Protocol", gp)
		}
	}
	if baseURL.User != nil {
		password, _ := baseURL.User.Password()
		httpReq.SetBasicAuth(baseURL.User.Username(), password)
	}
	if t.opts.Authorizer != nil {
		if err := t.opts.Authorizer(httpReq); err != nil {
			return nil, fmt.Errorf("http transport: authorize: %w", err)
		}
	}

	client := t.resolveClient()
	resp, err := doRequest(client, httpReq)
	if err != nil {
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
	sessReq := *req
	sessReq.URL = redirectedURL
	authorizer := t.opts.Authorizer

	// Clear credentials when the redirect landed on a different host.
	// The session stores baseURL and re-applies its User field and the
	// Authorizer callback on every subsequent POST — without this, the
	// original host's credentials would be sent to the new host.
	// Go's http.Client already strips the Authorization header on
	// cross-host redirects during the initial GET, but User and
	// Authorizer are carried in the session and applied explicitly by
	// doPost/applyAuth. In canonical git, credential_from_url()
	// re-derives credentials from the new URL, effectively wiping the
	// old ones.
	if redirectedURL.Host != baseURL.Host {
		sessReq.URL.User = nil
		authorizer = nil
	}

	if forceDumb {
		return handshakeDumb(resp, &sessReq, client, authorizer)
	}

	expected := fmt.Sprintf("application/x-%s-advertisement", service)
	isSmart := resp.Header.Get("Content-Type") == expected

	if isSmart {
		return handshakeSmart(resp, &sessReq, client, authorizer)
	}
	return handshakeDumb(resp, &sessReq, client, authorizer)
}

func handshakeSmart(resp *http.Response, req *transport.Request, client *http.Client, authorizer func(*http.Request) error) (transport.Session, error) {
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
		if reply.Service != req.Command {
			return nil, fmt.Errorf("unexpected service name: %w", transport.ErrInvalidResponse)
		}
	}

	ver, err := transport.DiscoverVersion(rd)
	if err != nil {
		return nil, err
	}
	switch ver {
	case protocol.V2:
		return nil, transport.ErrUnsupportedVersion
	case protocol.V1, protocol.V0:
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(rd); err != nil && !errors.Is(err, packp.ErrEmptyAdvRefs) {
		return nil, err
	}

	return &smartPackSession{
		client:     client,
		baseURL:    req.URL,
		service:    req.Command,
		authorizer: authorizer,
		version:    ver,
		caps:       ar.Capabilities,
		refs:       ar,
	}, nil
}

func handshakeDumb(resp *http.Response, req *transport.Request, client *http.Client, authorizer func(*http.Request) error) (transport.Session, error) {
	defer resp.Body.Close() //nolint:errcheck
	rd := bufio.NewReader(resp.Body)

	var infoRefs packp.InfoRefs
	if err := infoRefs.Decode(rd); err != nil {
		return nil, err
	}

	ar := packp.NewAdvRefs()
	ar.References = infoRefs.References
	ar.Peeled = infoRefs.Peeled

	return &dumbPackSession{
		client:     client,
		baseURL:    req.URL,
		service:    req.Command,
		authorizer: authorizer,
		refs:       ar,
	}, nil
}

// --- smart HTTP pack session ---

var _ transport.Session = (*smartPackSession)(nil)

type smartPackSession struct {
	client     *http.Client
	baseURL    *url.URL
	service    string
	authorizer func(*http.Request) error
	version    protocol.Version
	caps       *capability.List
	refs       *packp.AdvRefs
}

func (s *smartPackSession) Capabilities() *capability.List { return s.caps }

func (s *smartPackSession) GetRemoteRefs(_ context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, transport.ErrEmptyRemoteRepository
	}
	forPush := s.service == transport.ReceivePackService
	if !forPush && s.refs.IsEmpty() {
		return nil, transport.ErrEmptyRemoteRepository
	}
	return s.refs.MakeReferenceSlice()
}

func (s *smartPackSession) Fetch(ctx context.Context, st storage.Storer, req *transport.FetchRequest) error {
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
	neg.closeResponse()
	return err
}

func (s *smartPackSession) Push(ctx context.Context, st storage.Storer, req *transport.PushRequest) error {
	rwc := &httpRequester{session: s, ctx: ctx}
	err := transport.SendPack(ctx, st, s.caps, rwc, io.NopCloser(rwc), req)
	// Only close the response body on success. On error (especially context
	// cancellation), context-wrapper goroutines inside SendPack may still
	// be reading from it.
	if err == nil && rwc.resp != nil {
		_ = rwc.resp.Body.Close()
	}
	return err
}

func (s *smartPackSession) Close() error { return nil }

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
	if r.session.baseURL.User != nil {
		password, _ := r.session.baseURL.User.Password()
		httpReq.SetBasicAuth(r.session.baseURL.User.Username(), password)
	}
	if r.session.authorizer != nil {
		if err := r.session.authorizer(httpReq); err != nil {
			return err
		}
	}
	r.resp, err = doRequest(r.session.client, httpReq)
	if err != nil {
		return fmt.Errorf("http transport: %w", err)
	}
	if r.resp.StatusCode != http.StatusOK {
		_ = r.resp.Body.Close()
		return fmt.Errorf("http transport: POST %s unexpected status %d", serviceURL, r.resp.StatusCode)
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
		// Previous round is complete — close its response, start fresh.
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

// --- dumb HTTP pack session ---

var _ transport.Session = (*dumbPackSession)(nil)

type dumbPackSession struct {
	client     *http.Client
	baseURL    *url.URL
	service    string
	authorizer func(*http.Request) error
	refs       *packp.AdvRefs
}

func (s *dumbPackSession) Capabilities() *capability.List { return capability.NewList() }

func (s *dumbPackSession) GetRemoteRefs(_ context.Context) ([]*plumbing.Reference, error) {
	if s.refs == nil {
		return nil, transport.ErrEmptyRemoteRepository
	}
	return s.refs.MakeReferenceSlice()
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
