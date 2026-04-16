// Package client provides a convenience Client that resolves URL schemes
// to transport implementations and provides Handshake/Connect methods.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/file"
	xgit "github.com/go-git/go-git/v6/plumbing/transport/git"
	xhttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	xssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

// SSHAuth is implemented by SSH authentication types whose ClientConfig
// method can be used to produce an *ssh.ClientConfig for each request.
type SSHAuth interface {
	ClientConfig(context.Context, *transport.Request) (*gossh.ClientConfig, error)
}

// HTTPAuth is implemented by HTTP authentication types whose Authorizer
// method can be used to mutate outgoing HTTP requests.
type HTTPAuth interface {
	Authorizer(*http.Request) error
}

// RedirectPolicy controls how HTTP transports follow redirects.
type RedirectPolicy = xhttp.RedirectPolicy

const (
	// FollowInitialRedirects follows redirects only for the initial
	// /info/refs discovery request.
	FollowInitialRedirects = xhttp.FollowInitialRedirects
	// FollowRedirects follows redirects for all requests.
	FollowRedirects = xhttp.FollowRedirects
	// NoFollowRedirects disables redirects for all requests.
	NoFollowRedirects = xhttp.NoFollowRedirects
)

// Option configures a Client.
type Option func(*options)

type options struct {
	ssh  xssh.Options
	http xhttp.Options
	git  xgit.Options
	file file.Options

	schemes map[string]transport.Transport
}

func (o *options) ensureTLS() *tls.Config {
	if o.http.TLS == nil {
		o.http.TLS = &tls.Config{}
	}
	return o.http.TLS
}

// WithSSHAuth sets SSH authentication. The auth type's ClientConfig method
// is called for each SSH connection.
func WithSSHAuth(a SSHAuth) Option {
	return func(o *options) {
		o.ssh.ClientConfig = a.ClientConfig
	}
}

// WithHTTPAuth sets HTTP authentication. The auth type's Authorizer method
// is called for each outgoing HTTP request.
func WithHTTPAuth(a HTTPAuth) Option {
	return func(o *options) {
		o.http.Authorizer = a.Authorizer
	}
}

// WithProxyURL routes all transport connections through the given proxy URL.
// For HTTP, this uses http.ProxyURL. For SSH and Git TCP, this uses
// golang.org/x/net/proxy.FromURL to wrap the underlying dialer.
func WithProxyURL(u *url.URL) Option {
	return func(o *options) {
		o.http.HTTPProxy = http.ProxyURL(u)

		wrap := proxyDialer(func(forward proxy.Dialer) (proxy.Dialer, error) {
			return proxy.FromURL(u, forward)
		})
		o.ssh.DialProxy = wrap
		o.git.DialProxy = wrap
	}
}

// WithProxyEnvironment honors standard proxy environment variables
// (HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, NO_PROXY) for all transports.
// For HTTP, this uses http.ProxyFromEnvironment. For SSH and Git TCP,
// this uses golang.org/x/net/proxy.FromEnvironmentUsing.
func WithProxyEnvironment() Option {
	return func(o *options) {
		o.http.HTTPProxy = http.ProxyFromEnvironment

		wrap := proxyDialer(func(forward proxy.Dialer) (proxy.Dialer, error) {
			return proxy.FromEnvironmentUsing(forward), nil
		})
		o.ssh.DialProxy = wrap
		o.git.DialProxy = wrap
	}
}

// WithDialer sets a custom dialer for SSH and Git TCP transports.
func WithDialer(fn transport.DialContextFunc) Option {
	return func(o *options) {
		o.ssh.DialContext = fn
		o.git.DialContext = fn
	}
}

// WithHTTPClient sets the HTTP client used by the HTTP transport.
// When a custom client is set, WithInsecureSkipTLS, WithCABundle, and
// WithProxyURL/WithProxyEnvironment do not affect HTTP connections —
// configure them on the provided client directly.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) {
		o.http.Client = c
	}
}

// WithRedirectPolicy sets the HTTP redirect policy. If unset, the HTTP
// transport defaults to FollowInitialRedirects.
func WithRedirectPolicy(policy RedirectPolicy) Option {
	return func(o *options) {
		o.http.FollowRedirects = xhttp.RedirectPolicy(policy)
	}
}

// WithInsecureSkipTLS disables TLS certificate verification for HTTPS.
// Can be combined with WithCABundle.
func WithInsecureSkipTLS() Option {
	return func(o *options) {
		o.ensureTLS().InsecureSkipVerify = true
	}
}

// WithCABundle sets a PEM-encoded CA certificate bundle for HTTPS
// connections. When set, only these CAs are trusted.
// Can be combined with WithInsecureSkipTLS.
func WithCABundle(pem []byte) Option {
	return func(o *options) {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(pem)
		o.ensureTLS().RootCAs = pool
	}
}

// WithLoader sets the storage loader for the file transport.
func WithLoader(l transport.Loader) Option {
	return func(o *options) {
		o.file.Loader = l
	}
}

// WithTransport registers a custom transport for the given URL scheme.
// This overrides any built-in transport for that scheme.
func WithTransport(scheme string, tr transport.Transport) Option {
	return func(o *options) {
		if scheme == "" || tr == nil {
			return
		}
		if o.schemes == nil {
			o.schemes = make(map[string]transport.Transport)
		}
		o.schemes[scheme] = tr
	}
}

// Client resolves URL schemes to transport implementations.
type Client struct {
	opts options
}

// New creates a Client with built-in transports for file, git, ssh, http,
// and https schemes. Options customize authentication, proxying, dialing,
// and transport overrides.
func New(opts ...Option) *Client {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{opts: o}
}

// Handshake resolves the transport for the request URL scheme and performs
// a pack protocol handshake.
func (c *Client) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	tr, err := c.resolve(req)
	if err != nil {
		return nil, err
	}
	return tr.Handshake(ctx, req)
}

// Connect resolves the transport for the request URL scheme and opens a
// raw full-duplex connection. Returns ErrConnectUnsupported if the transport
// does not implement Connector (e.g. HTTP).
func (c *Client) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	tr, err := c.resolve(req)
	if err != nil {
		return nil, err
	}
	conn, ok := tr.(transport.Connector)
	if !ok {
		return nil, fmt.Errorf("transport for %s does not support Connect: %w", req.URL.Scheme, transport.ErrConnectUnsupported)
	}
	return conn.Connect(ctx, req)
}

// Transport returns the resolved transport for the given URL scheme.
func (c *Client) Transport(scheme string) (transport.Transport, error) {
	if c.opts.schemes != nil {
		if tr, ok := c.opts.schemes[scheme]; ok {
			return tr, nil
		}
	}
	return c.builtin(scheme)
}

// Close releases resources held by the client.
func (c *Client) Close() error {
	return nil
}

func (c *Client) resolve(req *transport.Request) (transport.Transport, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("transport: nil request or URL")
	}
	return c.Transport(req.URL.Scheme)
}

func (c *Client) builtin(scheme string) (transport.Transport, error) {
	switch scheme {
	case "file":
		return file.NewTransport(c.opts.file), nil
	case "git":
		return xgit.NewTransport(c.opts.git), nil
	case "ssh":
		return xssh.NewTransport(c.opts.ssh), nil
	case "http", "https":
		return xhttp.NewTransport(c.opts.http), nil
	default:
		return nil, fmt.Errorf("transport: unsupported scheme %q", scheme)
	}
}

// proxyDialer creates a DialProxy wrapper from a function that produces
// a proxy.Dialer given a forwarding proxy.Dialer.
func proxyDialer(makeDialer func(proxy.Dialer) (proxy.Dialer, error)) func(transport.DialContextFunc) transport.DialContextFunc {
	return func(direct transport.DialContextFunc) transport.DialContextFunc {
		d, err := makeDialer(direct)
		if err != nil {
			return direct
		}
		if cd, ok := d.(proxy.ContextDialer); ok {
			return cd.DialContext
		}
		return func(_ context.Context, network, addr string) (net.Conn, error) {
			return d.Dial(network, addr)
		}
	}
}
