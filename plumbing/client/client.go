// Package client provides a convenience Client that resolves URL schemes
// to transport implementations and provides Handshake/Connect methods.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/file"
	xgit "github.com/go-git/go-git/v6/plumbing/transport/git"
	xhttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	xssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

// ErrUnsupportedProxyScheme is returned when an SSH or Git connection is
// dialed through a proxy, set by WithProxyURL or by ALL_PROXY under
// WithProxyEnvironment, whose scheme golang.org/x/net/proxy cannot use, for
// example http. Only socks5 and socks5h are supported. The connection fails
// instead of silently bypassing the proxy. HTTP connections never return it.
var ErrUnsupportedProxyScheme = errors.New("unsupported proxy scheme")

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

// addCredentials adds fn to the HTTP transport's credential sources.
//
// The latest source is tried first, matching option precedence elsewhere in
// this package. Errors stop the chain; only a decline tries an earlier source.
func (o *options) addCredentials(fn xhttp.CredentialsFunc) {
	if o.http.Credentials == nil {
		o.http.Credentials = fn
		return
	}
	o.http.Credentials = xhttp.Chain(fn, o.http.Credentials)
}

// WithHTTPAuth sets HTTP authentication. The auth type's Authorizer method is
// called for each outgoing request made to the origin in the repository URL,
// and for no other origin except the permitted http:80 to https:443 upgrade
// on the same host. Use WithHTTPCredentials to authenticate a redirect's
// origin.
//
// It composes with WithHTTPCredentials; see there for precedence.
//
// An untyped nil a is ignored. A typed nil in a non-nil interface —
// WithHTTPAuth((*http.BasicAuth)(nil)) — is a value like any other and panics
// on the first authenticated request.
func WithHTTPAuth(a HTTPAuth) Option {
	return func(o *options) {
		if a == nil {
			return
		}
		o.addCredentials(xhttp.ForRepositoryOrigin(a.Authorizer))
	}
}

// CredentialsFunc supplies a credential for an origin the HTTP transport is
// about to make a request to. It is the type WithHTTPCredentials takes; see
// [github.com/go-git/go-git/v6/plumbing/transport/http.CredentialsFunc] for
// the contract and the adapters for building one.
type CredentialsFunc = xhttp.CredentialsFunc

// CredentialRequest identifies what a credential is wanted for. It is the
// argument of a CredentialsFunc.
type CredentialRequest = xhttp.CredentialRequest

// Credential is a credential for the origin that was asked about. It is what a
// CredentialsFunc returns.
type Credential = xhttp.Credential

// WithHTTPCredentials sets a per-origin credential source for the HTTP
// transport. It is consulted for the repository's origin and for a redirect
// target; see CredentialsFunc for the full contract.
//
// Multiple sources compose: the last option applied is consulted first, a
// decline falls through to earlier sources, and an error stops the chain and
// is returned. Userinfo in the repository URL is applied before the selected
// source, so the source can replace its Authorization header or add others.
//
// Options are applied per operation, so a fetch and a push can be given
// different sources by passing this in the ClientOptions of each. A credential
// is selected by origin rather than by operation, so that is where a caller
// holding separate read and write tokens draws the line.
//
// Prefer transport/http.ForRepositoryOrigin or ForOrigin, combined with Chain.
// A hand-written source can use CredentialRequest.IsOrigin for the transport's
// own origin comparison.
func WithHTTPCredentials(fn CredentialsFunc) Option {
	return func(o *options) {
		o.addCredentials(fn)
	}
}

// WithProxyURL routes all transport connections through the given proxy URL.
// For HTTP, this uses http.ProxyURL. For SSH and Git TCP, this uses
// golang.org/x/net/proxy.FromURL to wrap the underlying dialer, which only
// supports socks5 and socks5h. Any other scheme fails SSH and Git connections
// with ErrUnsupportedProxyScheme instead of bypassing the proxy.
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
// ALL_PROXY (or all_proxy) selects the proxy and NO_PROXY (or no_proxy)
// lists the hosts dialed directly, as golang.org/x/net/proxy.FromEnvironment
// does. If ALL_PROXY is set to a scheme other than socks5 or socks5h, SSH and
// Git connections to hosts not listed in NO_PROXY fail with
// ErrUnsupportedProxyScheme instead of bypassing the proxy.
//
// Unlike golang.org/x/net/proxy, which reads ALL_PROXY and NO_PROXY once per
// process, they are read again for every SSH and Git connection. An ALL_PROXY
// value that is not a valid URL fails SSH and Git connections to hosts not
// listed in NO_PROXY with an "invalid ALL_PROXY" error rather than dialing
// directly. That error does not wrap ErrUnsupportedProxyScheme, as no scheme
// could be read from the value.
func WithProxyEnvironment() Option {
	return func(o *options) {
		o.http.HTTPProxy = http.ProxyFromEnvironment

		wrap := proxyDialer(func(forward proxy.Dialer) (proxy.Dialer, error) {
			allProxy := getEnvAny("ALL_PROXY", "all_proxy")
			if allProxy == "" {
				return forward, nil
			}

			// x/net's FromEnvironmentUsing dials directly when ALL_PROXY
			// holds an unsupported scheme, so the dialer is built here.
			var d proxy.Dialer
			u, err := url.Parse(allProxy)
			if err != nil {
				d = failDialer{fmt.Errorf("invalid ALL_PROXY: %w", err)}
			} else if d, err = proxy.FromURL(u, forward); err != nil {
				d = failDialer{fmt.Errorf("%w: %w", ErrUnsupportedProxyScheme, err)}
			}

			noProxy := getEnvAny("NO_PROXY", "no_proxy")
			if noProxy == "" {
				return d, nil
			}
			perHost := proxy.NewPerHost(d, forward)
			perHost.AddFromString(noProxy)
			return perHost, nil
		})
		o.ssh.DialProxy = wrap
		o.git.DialProxy = wrap
	}
}

// getEnvAny returns the value of the first non-empty environment variable
// in names.
func getEnvAny(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// failDialer is a proxy.Dialer that fails every dial with err.
type failDialer struct{ err error }

func (f failDialer) Dial(string, string) (net.Conn, error) { return nil, f.err }

func (f failDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, f.err
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
// a proxy.Dialer given a forwarding proxy.Dialer. If no dialer can be
// produced, the returned dial func fails rather than dialing directly.
func proxyDialer(makeDialer func(proxy.Dialer) (proxy.Dialer, error)) func(transport.DialContextFunc) transport.DialContextFunc {
	return func(direct transport.DialContextFunc) transport.DialContextFunc {
		d, err := makeDialer(direct)
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrUnsupportedProxyScheme, err)
			return func(context.Context, string, string) (net.Conn, error) {
				return nil, err
			}
		}
		if cd, ok := d.(proxy.ContextDialer); ok {
			return cd.DialContext
		}
		return func(_ context.Context, network, addr string) (net.Conn, error) {
			return d.Dial(network, addr)
		}
	}
}
