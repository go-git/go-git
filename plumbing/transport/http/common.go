// Package http implements the HTTP transport protocol.
package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/utils/ioutil"
	"github.com/golang/groupcache/lru"
)

func init() {
	transport.Register("http", DefaultClient)
	transport.Register("https", DefaultClient)
}

func applyHeaders(
	req *http.Request,
	service string,
	ep *transport.Endpoint,
	auth AuthMethod,
	useSmart bool,
) {
	// Add headers
	req.Header.Add("User-Agent", "git/1.0")
	req.Header.Add("Host", ep.Host) // host:port

	if useSmart {
		req.Header.Add("Accept", fmt.Sprintf("application/x-%s-result", service))
		req.Header.Add("Content-Type", fmt.Sprintf("application/x-%s-request", service))
	}

	// Set auth headers
	if auth != nil {
		auth.SetAuth(req)
	}
}

// doRequest applies the auth and headers, then performs a request to the
// server and returns the response.
func doRequest(
	client *http.Client,
	req *http.Request,
) (*http.Response, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return res, nil
	}

	return nil, checkError(res)
}

// modifyRedirect modifies the endpoint based on the redirect response.
func modifyRedirect(res *http.Response, ep *transport.Endpoint) {
	if res.Request == nil {
		return
	}

	r := res.Request
	if !strings.HasSuffix(r.URL.Path, infoRefsPath) {
		return
	}

	h, p, err := net.SplitHostPort(r.URL.Host)
	if err != nil {
		h = r.URL.Host
	}
	if p != "" {
		port, err := strconv.Atoi(p)
		if err == nil {
			ep.Port = port
		}
	}

	ep.Host = h
	ep.Protocol = r.URL.Scheme
	ep.Path = r.URL.Path[:len(r.URL.Path)-len(infoRefsPath)]
}

// it requires a bytes.Buffer, because we need to know the length
func applyHeadersToRequest(req *http.Request, content *bytes.Buffer, host string, requestType string) {
	req.Header.Add("User-Agent", "git/1.0")
	req.Header.Add("Host", host) // host:port

	if content == nil {
		req.Header.Add("Accept", "*/*")
		return
	}

	req.Header.Add("Accept", fmt.Sprintf("application/x-%s-result", requestType))
	req.Header.Add("Content-Type", fmt.Sprintf("application/x-%s-request", requestType))
	req.Header.Add("Content-Length", strconv.Itoa(content.Len()))
}

const infoRefsPath = "/info/refs"

func advertisedReferences(ctx context.Context, s *session, serviceName string) (ref *packp.AdvRefs, err error) {
	url := fmt.Sprintf(
		"%s%s?service=%s",
		s.ep.String(), infoRefsPath, serviceName,
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	s.ApplyAuthToRequest(req)
	applyHeadersToRequest(req, nil, s.ep.Host, serviceName)
	res, err := s.client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	s.ModifyEndpointIfRedirect(res)
	defer ioutil.CheckClose(res.Body, &err)

	if err = checkError(res); err != nil {
		return nil, err
	}

	ar := packp.NewAdvRefs()
	if err = ar.Decode(res.Body); err != nil {
		if err == packp.ErrEmptyAdvRefs {
			err = transport.ErrEmptyRemoteRepository
		}

		return nil, err
	}

	// Git 2.41+ returns a zero-id plus capabilities when an empty
	// repository is being cloned. This skips the existing logic within
	// advrefs_decode.decodeFirstHash, which expects a flush-pkt instead.
	//
	// This logic aligns with plumbing/transport/internal/common/common.go.
	if ar.IsEmpty() &&
		// Empty repositories are valid for git-receive-pack.
		transport.ReceivePackServiceName != serviceName {
		return nil, transport.ErrEmptyRemoteRepository
	}

	transport.FilterUnsupportedCapabilities(ar.Capabilities)
	s.advRefs = ar

	return ar, nil
}

type client struct {
	client     *http.Client
	transports *lru.Cache
	mutex      sync.RWMutex
	useDumb    bool // When true, the client will always use the dumb protocol.
}

// ClientOptions holds user configurable options for the client.
type ClientOptions struct {
	// CacheMaxEntries is the max no. of entries that the transport objects
	// cache will hold at any given point of time. It must be a positive integer.
	// Calling `client.addTransport()` after the cache has reached the specified
	// size, will result in the least recently used transport getting deleted
	// before the provided transport is added to the cache.
	CacheMaxEntries int

	// UseDumb is a flag that when set to true, the client will always use the
	// dumb protocol.
	UseDumb bool
}

var (
	// defaultTransportCacheSize is the default capacity of the transport objects cache.
	// Its value is 0 because transport caching is turned off by default and is an
	// opt-in feature.
	defaultTransportCacheSize = 0

	// DefaultClient is the default HTTP client, which uses a net/http client configured
	// with http.DefaultTransport.
	DefaultClient = NewClient(nil)
)

// NewClient creates a new client with a custom net/http client.
// See `InstallProtocol` to install and override default http client.
// If the net/http client is nil or empty, it will use a net/http client configured
// with http.DefaultTransport.
//
// Note that for HTTP client cannot distinguish between private repositories and
// unexistent repositories on GitHub. So it returns `ErrAuthorizationRequired`
// for both.
func NewClient(c *http.Client) transport.Transport {
	if c == nil {
		c = &http.Client{
			Transport: http.DefaultTransport,
		}
	}
	return NewClientWithOptions(c, &ClientOptions{
		CacheMaxEntries: defaultTransportCacheSize,
	})
}

// NewClientWithOptions returns a new client configured with the provided net/http client
// and other custom options specific to the client.
// If the net/http client is nil or empty, it will use a net/http client configured
// with http.DefaultTransport.
func NewClientWithOptions(c *http.Client, opts *ClientOptions) transport.Transport {
	if c == nil {
		c = &http.Client{
			Transport: http.DefaultTransport,
		}
	}
	cl := &client{
		client:  c,
		useDumb: opts.UseDumb,
	}

	if opts != nil {
		if opts.CacheMaxEntries > 0 {
			cl.transports = lru.New(opts.CacheMaxEntries)
		}
	}
	return cl
}

func (c *client) NewSession(st storage.Storer, ep *transport.Endpoint, auth transport.AuthMethod) (transport.PackSession, error) {
	return newSession(st, c, ep, auth, c.useDumb)
}

func (c *client) NewUploadPackSession(ep *transport.Endpoint, auth transport.AuthMethod) (
	transport.UploadPackSession, error,
) {
	return newUploadPackSession(c, ep, auth)
}

func (c *client) NewReceivePackSession(ep *transport.Endpoint, auth transport.AuthMethod) (
	transport.ReceivePackSession, error,
) {
	return newReceivePackSession(c, ep, auth)
}

// TODO: support smart client
type session struct {
	st      storage.Storer
	auth    AuthMethod
	client  *http.Client
	ep      *transport.Endpoint
	advRefs *packp.AdvRefs
	version protocol.Version
	useDumb bool // When true, the client will always use the dumb protocol
	isSmart bool // This is true if the session is using the smart protocol
}

// IsSmart returns true if the session is using the smart protocol.
func (s *session) IsSmart() bool {
	return s.isSmart && !s.useDumb
}

var _ transport.PackSession = (*session)(nil)

func transportWithInsecureTLS(transport *http.Transport) {
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.InsecureSkipVerify = true
}

func transportWithCABundle(transport *http.Transport, caBundle []byte) error {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return err
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	rootCAs.AppendCertsFromPEM(caBundle)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = rootCAs
	return nil
}

func transportWithProxy(transport *http.Transport, proxyURL *url.URL) {
	transport.Proxy = http.ProxyURL(proxyURL)
}

func configureTransport(transport *http.Transport, ep *transport.Endpoint) error {
	if len(ep.CaBundle) > 0 {
		if err := transportWithCABundle(transport, ep.CaBundle); err != nil {
			return err
		}
	}
	if ep.InsecureSkipTLS {
		transportWithInsecureTLS(transport)
	}

	if ep.Proxy.URL != "" {
		proxyURL, err := ep.Proxy.FullURL()
		if err != nil {
			return err
		}
		transportWithProxy(transport, proxyURL)
	}
	return nil
}

func newSession(st storage.Storer, c *client, ep *transport.Endpoint, auth transport.AuthMethod, useDumb bool) (*session, error) {
	var httpClient *http.Client

	// We need to configure the http transport if there are transport specific
	// options present in the endpoint.
	if len(ep.CaBundle) > 0 || ep.InsecureSkipTLS || ep.Proxy.URL != "" {
		var transport *http.Transport
		// if the client wasn't configured to have a cache for transports then just configure
		// the transport and use it directly, otherwise try to use the cache.
		if c.transports == nil {
			tr, ok := c.client.Transport.(*http.Transport)
			if !ok {
				return nil, fmt.Errorf("expected underlying client transport to be of type: %s; got: %s",
					reflect.TypeOf(transport), reflect.TypeOf(c.client.Transport))
			}

			transport = tr.Clone()
			configureTransport(transport, ep)
		} else {
			transportOpts := transportOptions{
				caBundle:        string(ep.CaBundle),
				insecureSkipTLS: ep.InsecureSkipTLS,
			}
			if ep.Proxy.URL != "" {
				proxyURL, err := ep.Proxy.FullURL()
				if err != nil {
					return nil, err
				}
				transportOpts.proxyURL = *proxyURL
			}
			var found bool
			transport, found = c.fetchTransport(transportOpts)

			if !found {
				transport = c.client.Transport.(*http.Transport).Clone()
				configureTransport(transport, ep)
				c.addTransport(transportOpts, transport)
			}
		}

		httpClient = &http.Client{
			Transport:     transport,
			CheckRedirect: c.client.CheckRedirect,
			Jar:           c.client.Jar,
			Timeout:       c.client.Timeout,
		}
	} else {
		httpClient = c.client
	}

	s := &session{
		st:      st,
		auth:    basicAuthFromEndpoint(ep),
		client:  httpClient,
		ep:      ep,
		useDumb: useDumb,
	}
	if auth != nil {
		a, ok := auth.(AuthMethod)
		if !ok {
			return nil, transport.ErrInvalidAuthMethod
		}

		s.auth = a
	}

	return s, nil
}

// Handshake implements transport.PackSession.
func (s *session) Handshake(ctx context.Context, forPush bool, params ...string) (transport.Connection, error) {
	service := transport.UploadPackServiceName
	if forPush {
		service = transport.ReceivePackServiceName
	}

	url := s.ep.String() + infoRefsPath
	if !s.useDumb {
		url += "?service=" + service
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	applyHeaders(req, service, s.ep, s.auth, !s.useDumb)
	res, err := doRequest(s.client, req)
	if err != nil {
		return nil, err
	}

	if contentType := res.Header.Get("Content-Type"); !s.useDumb {
		s.isSmart = contentType == fmt.Sprintf("application/x-%s-advertisement", service)
	}

	modifyRedirect(res, s.ep)
	defer ioutil.CheckClose(res.Body, &err)

	rd := bufio.NewReader(res.Body)
	s.version, _ = transport.DiscoverVersion(rd)

	ar := packp.NewAdvRefs()
	if err = ar.Decode(rd); err != nil {
		if err == packp.ErrEmptyAdvRefs {
			err = transport.ErrEmptyRemoteRepository
		}

		return nil, err
	}

	// Git 2.41+ returns a zero-id plus capabilities when an empty
	// repository is being cloned. This skips the existing logic within
	// advrefs_decode.decodeFirstHash, which expects a flush-pkt instead.
	//
	// This logic aligns with plumbing/transport/internal/common/common.go.
	if ar.IsEmpty() &&
		// Empty repositories are valid for git-receive-pack.
		transport.ReceivePackServiceName != service {
		return nil, transport.ErrEmptyRemoteRepository
	}

	transport.FilterUnsupportedCapabilities(ar.Capabilities)
	s.advRefs = ar

	return s, nil
}

var _ transport.Connection = &session{}

// Capabilities implements transport.Connection.
func (s *session) Capabilities() *capability.List {
	return s.advRefs.Capabilities
}

// IsStatelessRPC implements transport.Connection.
func (*session) IsStatelessRPC() bool {
	return true
}

// Fetch implements transport.Connection.
func (s *session) Fetch(ctx context.Context, req *transport.FetchRequest) (err error) {
	rwc := newRequester(ctx, s, false)

	// XXX: packfile will be populated and accessible once rwc.Close() is
	// called in NegotiatePack.
	packfile := rwc.BodyCloser()
	shallows, err := transport.NegotiatePack(s.st, s, packfile, rwc, req)
	if err != nil {
		if rwc.res != nil {
			// Make sure the response body is closed.
			defer packfile.Close() // nolint: errcheck
		}
		return err
	}

	return transport.FetchPack(ctx, s.st, s, packfile, shallows, req)
}

// GetRemoteRefs implements transport.Connection.
func (s *session) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	refs, err := s.advRefs.AllReferences()
	if err != nil {
		return nil, err
	}

	// FIXME: this is a bit of a hack, to fix this, we need to redefine and
	// simplify AdvRefs.
	var allRefs []*plumbing.Reference
	for _, ref := range refs {
		allRefs = append(allRefs, ref)
	}
	for name, hash := range s.advRefs.Peeled {
		allRefs = append(allRefs,
			plumbing.NewReferenceFromStrings(name+"^{}", hash.String()),
		)
	}

	return allRefs, nil
}

// Push implements transport.Connection.
func (s *session) Push(ctx context.Context, req *transport.PushRequest) (err error) {
	rwc := newRequester(ctx, s, true)
	return transport.SendPack(ctx, s.st, s, rwc, rwc.BodyCloser(), req)
}

// Version implements transport.Connection.
func (s *session) Version() protocol.Version {
	return s.version
}

// requester is a io.WriteCloser that sends an HTTP request to on close and
// reads the response into the struct.
type requester struct {
	*session

	reqBuf bytes.Buffer
	ctx    context.Context
	req    *http.Request  // the last request made
	res    *http.Response // the last response received
	done   bool           // indicates if the request has been done without errors

	service string
}

func newRequester(ctx context.Context, s *session, forPush bool) *requester {
	service := transport.UploadPackServiceName
	if forPush {
		service = transport.ReceivePackServiceName
	}

	return &requester{
		ctx:     ctx,
		session: s,
		service: service,
	}
}

var _ io.ReadWriteCloser = &requester{}

// BodyCloser returns the response body as an io.ReadCloser.
func (r *requester) BodyCloser() io.ReadCloser {
	return ioutil.NewReadCloser(r, ioutil.CloserFunc(func() error {
		if r.res == nil {
			panic("http: requester.res is accessed before requester.Close")
		}
		return r.res.Body.Close()
	}))
}

// Read implements io.ReadWriteCloser.
func (r *requester) Read(p []byte) (n int, err error) {
	if r.res == nil {
		panic("http: requester.Read called before requester.Close")
	}
	return r.res.Body.Read(p)
}

// Close implements io.ReadWriteCloser.
func (r *requester) Close() error {
	defer r.reqBuf.Reset()

	url := fmt.Sprintf("%s/%s", r.ep.String(), r.service)
	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, url, &r.reqBuf)
	if err != nil {
		return err
	}

	applyHeaders(req, r.service, r.ep, r.auth, r.IsSmart())
	res, err := doRequest(r.client, req)
	if err != nil {
		return err
	}

	r.res = res
	r.req = req

	return nil
}

// Write implements io.ReadWriteCloser.
func (r *requester) Write(p []byte) (n int, err error) {
	return r.reqBuf.Write(p)
}

func (s *session) ApplyAuthToRequest(req *http.Request) {
	if s.auth == nil {
		return
	}

	s.auth.SetAuth(req)
}

func (s *session) ModifyEndpointIfRedirect(res *http.Response) {
	if res.Request == nil {
		return
	}

	r := res.Request
	if !strings.HasSuffix(r.URL.Path, infoRefsPath) {
		return
	}

	h, p, err := net.SplitHostPort(r.URL.Host)
	if err != nil {
		h = r.URL.Host
	}
	if p != "" {
		port, err := strconv.Atoi(p)
		if err == nil {
			s.ep.Port = port
		}
	}
	s.ep.Host = h

	s.ep.Protocol = r.URL.Scheme
	s.ep.Path = r.URL.Path[:len(r.URL.Path)-len(infoRefsPath)]
}

func (*session) Close() error {
	return nil
}

// AuthMethod is concrete implementation of common.AuthMethod for HTTP services
type AuthMethod interface {
	transport.AuthMethod
	SetAuth(r *http.Request)
}

func basicAuthFromEndpoint(ep *transport.Endpoint) *BasicAuth {
	u := ep.User
	if u == "" {
		return nil
	}

	return &BasicAuth{u, ep.Password}
}

// BasicAuth represent a HTTP basic auth
type BasicAuth struct {
	Username, Password string
}

func (a *BasicAuth) SetAuth(r *http.Request) {
	if a == nil {
		return
	}

	r.SetBasicAuth(a.Username, a.Password)
}

// Name is name of the auth
func (a *BasicAuth) Name() string {
	return "http-basic-auth"
}

func (a *BasicAuth) String() string {
	masked := "*******"
	if a.Password == "" {
		masked = "<empty>"
	}

	return fmt.Sprintf("%s - %s:%s", a.Name(), a.Username, masked)
}

// TokenAuth implements an http.AuthMethod that can be used with http transport
// to authenticate with HTTP token authentication (also known as bearer
// authentication).
//
// IMPORTANT: If you are looking to use OAuth tokens with popular servers (e.g.
// GitHub, Bitbucket, GitLab) you should use BasicAuth instead. These servers
// use basic HTTP authentication, with the OAuth token as user or password.
// Check the documentation of your git server for details.
type TokenAuth struct {
	Token string
}

func (a *TokenAuth) SetAuth(r *http.Request) {
	if a == nil {
		return
	}
	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.Token))
}

// Name is name of the auth
func (a *TokenAuth) Name() string {
	return "http-token-auth"
}

func (a *TokenAuth) String() string {
	masked := "*******"
	if a.Token == "" {
		masked = "<empty>"
	}
	return fmt.Sprintf("%s - %s", a.Name(), masked)
}

// Err is a dedicated error to return errors based on status code
type Err struct {
	URL    *url.URL
	Status int
	Reason string
}

// checkError returns a new Err based on a http response.
func checkError(r *http.Response) error {
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var reason string

	// If a response message is present, add it to error
	var messageBuffer bytes.Buffer
	if r.Body != nil {
		messageLength, _ := messageBuffer.ReadFrom(r.Body)
		if messageLength > 0 {
			reason = messageBuffer.String()
		}
	}

	switch r.StatusCode {
	case http.StatusUnauthorized:
		return transport.ErrAuthenticationRequired
	case http.StatusForbidden:
		return transport.ErrAuthorizationFailed
	case http.StatusNotFound:
		return transport.ErrRepositoryNotFound
	}

	return plumbing.NewUnexpectedError(&Err{
		URL:    r.Request.URL,
		Status: r.StatusCode,
		Reason: reason,
	})
}

// StatusCode returns the status code of the response
func (e *Err) StatusCode() int {
	return e.Status
}

func (e *Err) Error() string {
	format := "unexpected requesting %q status code: %d"
	if e.Reason != "" {
		return fmt.Sprintf(format+": %s", e.URL, e.Status, e.Reason)
	}
	return fmt.Sprintf(format, e.URL, e.Status)
}
