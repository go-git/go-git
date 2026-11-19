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
// service is not enabled. Serve writes nothing to the client in that case.
// git daemon answers it with the pkt-line
// "ERR access denied or repository not exported: <dir>" (daemon.c
// daemon_error), and a git:// server should send the same, since a client
// that gets no reply cannot tell why the connection closed.
var ErrServiceNotEnabled = errors.New("service not enabled")

var errNilLoader = errors.New("backend: nil loader")

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
	// Loader resolves repository URLs to storage. It is required.
	// Requests fail while it is nil.
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
	// nil error authorizes the request, even one that carries no
	// credentials, and that opens receive-pack and upload-archive to it.
	// Implementations must therefore check the credentials themselves,
	// and return [ErrUnauthorized] when there are none. This matches git
	// http-backend, which serves receive-pack whenever REMOTE_USER is
	// non-empty and trusts the web server to have set it.
	// [ErrUnauthorized] answers 401 with a WWW-Authenticate challenge,
	// [ErrForbidden] answers 403, and any other error answers 500 without
	// exposing the error to the client. If nil, no request is authorized,
	// so receive-pack and upload-archive are only served when enabled
	// explicitly.
	Authorize func(r *http.Request, service string) error

	// ReceivePackHooks are the server-side hooks run for every
	// receive-pack request, over any transport. The zero value runs none.
	ReceivePackHooks transport.ReceivePackHooks
}

// New creates a Backend that resolves repositories with l. It returns an
// error if l is nil, so that a missing loader is not silently replaced by
// one that can reach any repository on the host.
//
// New only detects a nil interface. A nil pointer of a concrete loader
// type, such as a nil *[transport.FilesystemLoader], is not nil as a
// [transport.Loader], so New accepts it, and the first request that
// reaches the loader panics. Detecting it would take reflection on every
// loader type, so New leaves that to the caller.
func New(l transport.Loader) (*Backend, error) {
	if l == nil {
		return nil, errNilLoader
	}
	return &Backend{
		Loader: l,
	}, nil
}

// Serve handles a Git pack protocol request. It resolves the repository
// from req.URL, validates the service, and runs the appropriate server
// command (upload-pack or receive-pack).
//
// Serve applies git daemon's service policy: upload-pack is served unless
// disabled, receive-pack and upload-archive only when enabled. A service
// that is not enabled fails with [ErrServiceNotEnabled], and an unknown
// service with [transport.ErrUnsupportedService], both before the
// repository is loaded. Serve does no authentication. Callers that
// authenticate the connection on their own, such as an SSH server, enable
// the services the client may use.
//
// The caller is responsible for closing r and w. Errors are returned, not
// written to the wire. The caller decides the error format (pkt-line for
// TCP/SSH, HTTP status for HTTP). In particular, a refused service writes
// nothing to w, so the caller must tell the client itself. See
// [ErrServiceNotEnabled] for what git daemon sends.
func (b *Backend) Serve(ctx context.Context, r io.ReadCloser, w io.WriteCloser, req *Request) error {
	if req.URL == nil {
		return fmt.Errorf("nil request URL")
	}
	enabled, known := b.daemonEnabled(req.Service)
	if !known {
		return fmt.Errorf("%w: %s", transport.ErrUnsupportedService, req.Service)
	}
	if !enabled {
		b.logf("%q: service not enabled", req.Service)
		return fmt.Errorf("%w: %s", ErrServiceNotEnabled, req.URL.Path)
	}
	return b.serve(ctx, r, w, req)
}

// daemonEnabled reports whether service is enabled under git daemon's
// policy (daemon.c daemon_service), and whether service is one Serve knows
// at all. Like git daemon, Serve rejects an unknown service before it looks
// up the repository, so the answer does not reveal whether it exists.
func (b *Backend) daemonEnabled(service string) (enabled, known bool) {
	switch service {
	case transport.UploadPackService:
		return b.UploadPack != ServiceDisabled, true
	case transport.ReceivePackService:
		return b.ReceivePack == ServiceEnabled, true
	case transport.UploadArchiveService:
		return b.UploadArchive == ServiceEnabled, true
	default:
		return false, false
	}
}

// serve runs the service for req without applying any service policy.
func (b *Backend) serve(ctx context.Context, r io.ReadCloser, w io.WriteCloser, req *Request) error {
	if b.Loader == nil {
		return errNilLoader
	}

	st, err := b.Loader.Load(req.URL)
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
			Hooks:         b.ReceivePackHooks,
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
