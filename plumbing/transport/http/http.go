// Package http implements the HTTP transport for the new transport API.
package http

import (
	"crypto/tls"
	"net/http"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

// Options configures the HTTP transport.
type Options struct {
	// Client is the underlying HTTP client. If nil, a default client is
	// created. When Client is set, TLS and HTTPProxy are ignored —
	// configure them on the provided Client directly.
	Client *http.Client

	// Authorizer mutates outgoing HTTP requests to add authentication.
	Authorizer func(*http.Request) error

	// HTTPProxy returns the proxy URL for a given HTTP request.
	// If nil, the default http.Transport proxy behavior is used.
	// Ignored when Client is set.
	HTTPProxy func(*http.Request) (*url.URL, error)

	// TLS configures TLS for HTTPS connections. Set InsecureSkipVerify
	// to skip certificate verification, or set RootCAs for a custom CA
	// bundle. Ignored when Client is set.
	TLS *tls.Config

	// ForceDumb forces the transport to use the dumb HTTP protocol,
	// bypassing smart HTTP detection. When true, the transport will
	// not send the ?service= query parameter in the info/refs request
	// and will always treat the server as a dumb HTTP server.
	ForceDumb bool
}

// Transport implements the http:// and https:// transport protocol.
type Transport struct {
	opts Options
}

var _ transport.Transport = (*Transport)(nil)

// NewTransport creates an HTTP transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

func (t *Transport) resolveClient() *http.Client {
	if t.opts.Client != nil {
		return t.opts.Client
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()

	if t.opts.HTTPProxy != nil {
		tr.Proxy = t.opts.HTTPProxy
	}

	if t.opts.TLS != nil {
		tr.TLSClientConfig = t.opts.TLS
	}

	return &http.Client{Transport: tr}
}
