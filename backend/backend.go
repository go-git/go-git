// Package backend provides a unified Git server backend that handles
// git-upload-pack and git-receive-pack over any transport (TCP, HTTP, SSH).
//
// Use [Backend.Serve] or [Backend.ServeConn] for stream-based transports
// (TCP, SSH, pipes). Use [Backend.ServeHTTP] for smart HTTP.
package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/go-git/go-git/v6/internal/transport/loader"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// Request describes a Git server-side operation.
type Request struct {
	// URL identifies the target repository. Loader.Load receives this
	// directly, so callers must ensure the scheme and path are set
	// appropriately for the configured Loader.
	URL *url.URL

	// Service is the Git command: "git-upload-pack" or "git-receive-pack".
	Service string

	// GitProtocol is the value of the GIT_PROTOCOL environment variable
	// (e.g. "version=2"). Empty means protocol v0.
	GitProtocol string

	// AdvertiseRefs, when true, causes the server to advertise references
	// without performing the full pack exchange. Used by HTTP smart
	// protocol for the info/refs endpoint.
	AdvertiseRefs bool

	// StatelessRPC, when true, indicates that the server should operate
	// in stateless request-response mode. Used by HTTP smart protocol.
	StatelessRPC bool
}

// ErrServiceNotEnabled is returned by [Backend.Serve] when the requested
// service is not enabled. The message matches the one git daemon sends to
// the client in that case.
var ErrServiceNotEnabled = errors.New("service not enabled")

// ServiceState controls whether a [Backend] serves a service. The zero
// value, [ServiceDefault], applies the default of the transport serving
// the request, which follows C git:
//
//   - [Backend.Serve] (git daemon): upload-pack is enabled, receive-pack
//     and upload-archive are disabled.
//   - [Backend.ServeHTTP] (git http-backend): upload-pack is enabled,
//     receive-pack and upload-archive are enabled only for requests that
//     [Backend.Authorize] accepts.
type ServiceState int8

const (
	// ServiceDefault applies the transport's default for the service.
	ServiceDefault ServiceState = iota
	// ServiceEnabled always serves the service, the same as setting
	// daemon.<service> or http.<service> to true in git.
	ServiceEnabled
	// ServiceDisabled never serves the service, the same as setting
	// daemon.<service> or http.<service> to false in git.
	ServiceDisabled
)

// Backend is a Git server that dispatches upload-pack and receive-pack
// requests. It implements [http.Handler] for HTTP and provides [Serve]
// and [ServeConn] for stream-based transports.
type Backend struct {
	// Loader resolves repository URLs to storage. If nil, a filesystem
	// loader that resolves URL paths against the root of the OS
	// filesystem is used.
	Loader transport.Loader

	// ErrorLog is used to log errors. If nil, errors are not logged.
	ErrorLog *log.Logger

	// Prefix is an HTTP path prefix stripped before route matching.
	// Only used by [ServeHTTP].
	Prefix string

	// UploadPack controls whether git-upload-pack (fetch and clone) is
	// served. See [ServiceState] for the defaults.
	UploadPack ServiceState

	// ReceivePack controls whether git-receive-pack (push) is served.
	// See [ServiceState] for the defaults.
	ReceivePack ServiceState

	// UploadArchive controls whether git-upload-archive is served. See
	// [ServiceState] for the defaults.
	UploadArchive ServiceState

	// Authorize decides whether an HTTP request may use service, one of
	// the git-* service names in [transport]. It is called by
	// [Backend.ServeHTTP] for every smart HTTP request to a service that
	// is not disabled, and is the only authentication ServeHTTP does. A
	// nil error authorizes the request. [ErrUnauthorized] answers 401
	// with a WWW-Authenticate challenge, [ErrForbidden] answers 403, and
	// any other error answers 500 without exposing the error to the
	// client. If nil, no request is authorized, so receive-pack and
	// upload-archive are only served when enabled explicitly.
	Authorize func(r *http.Request, service string) error
}

// New creates a Backend with the given loader. If l is nil, a filesystem
// loader that resolves URL paths against the root of the OS filesystem is
// used.
func New(l transport.Loader) *Backend {
	if l == nil {
		l = loader.Default()
	}
	return &Backend{
		Loader: l,
	}
}

// Serve handles a Git pack protocol request. It resolves the repository
// from req.URL, validates the service, and runs the appropriate server
// command (upload-pack or receive-pack).
//
// Serve applies git daemon's service policy: upload-pack is served unless
// disabled, receive-pack and upload-archive only when enabled. A service
// that is not enabled fails with [ErrServiceNotEnabled] before the
// repository is loaded. Serve does no authentication. Callers that
// authenticate the connection on their own, such as an SSH server, enable
// the services the client may use.
//
// The caller is responsible for closing r and w. Errors are returned, not
// written to the wire. The caller decides the error format (pkt-line for
// TCP/SSH, HTTP status for HTTP).
func (b *Backend) Serve(ctx context.Context, r io.ReadCloser, w io.WriteCloser, req *Request) error {
	if req.URL == nil {
		return fmt.Errorf("nil request URL")
	}
	if !b.daemonEnabled(req.Service) {
		b.logf("%q: service not enabled", req.Service)
		return fmt.Errorf("%w: %s", ErrServiceNotEnabled, req.URL.Path)
	}
	return b.serve(ctx, r, w, req)
}

// daemonEnabled reports whether service is enabled under git daemon's
// policy (daemon.c daemon_service). Unknown services are let through so
// serve reports them as unsupported.
func (b *Backend) daemonEnabled(service string) bool {
	switch service {
	case transport.UploadPackService:
		return b.UploadPack != ServiceDisabled
	case transport.ReceivePackService:
		return b.ReceivePack == ServiceEnabled
	case transport.UploadArchiveService:
		return b.UploadArchive == ServiceEnabled
	default:
		return true
	}
}

// serve runs the service for req without applying any service policy.
func (b *Backend) serve(ctx context.Context, r io.ReadCloser, w io.WriteCloser, req *Request) error {
	l := b.Loader
	if l == nil {
		l = loader.Default()
	}

	st, err := l.Load(req.URL)
	if err != nil {
		return err
	}
	defer func() {
		if closer, ok := st.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	// Transport commands may close their streams when an exchange finishes.
	// Serve leaves the underlying streams open for the caller.
	if r != nil {
		r = io.NopCloser(r)
	}
	if w != nil {
		w = ioutil.WriteNopCloser(w)
	}

	switch req.Service {
	case transport.UploadPackService:
		return transport.UploadPack(ctx, st, r, w, &transport.UploadPackRequest{
			GitProtocol:   req.GitProtocol,
			AdvertiseRefs: req.AdvertiseRefs,
			StatelessRPC:  req.StatelessRPC,
		})
	case transport.ReceivePackService:
		return transport.ReceivePack(ctx, st, r, w, &transport.ReceivePackRequest{
			GitProtocol:   req.GitProtocol,
			AdvertiseRefs: req.AdvertiseRefs,
			StatelessRPC:  req.StatelessRPC,
		})
	case transport.UploadArchiveService:
		return transport.UploadArchive(ctx, st, r, w, &transport.UploadArchiveRequest{})
	default:
		return fmt.Errorf("%w: %s", transport.ErrUnsupportedService, req.Service)
	}
}

func (b *Backend) logf(format string, v ...any) {
	if b.ErrorLog != nil {
		b.ErrorLog.Printf(format, v...)
	}
}

// RequestFromProto converts a Git protocol v0/v1 request (as received
// over TCP by git-daemon) into a [Request].
func RequestFromProto(proto *packp.GitProtoRequest) *Request {
	host := proto.Host
	if host == "" {
		host = "localhost"
	}

	u := &url.URL{
		Scheme: "git",
		Host:   host,
		Path:   proto.Pathname,
	}

	var gitProtocol string
	for _, p := range proto.ExtraParams {
		if gitProtocol == "" {
			gitProtocol = p
		} else {
			gitProtocol += ":" + p
		}
	}

	return &Request{
		URL:         u,
		Service:     proto.RequestCommand,
		GitProtocol: gitProtocol,
	}
}
