// Package backend provides a unified Git server backend that handles
// git-upload-pack and git-receive-pack over any transport (TCP, HTTP, SSH).
//
// Use [Backend.Serve] or [Backend.ServeConn] for stream-based transports
// (TCP, SSH, pipes). Use [Backend.ServeHTTP] for HTTP (both smart and dumb
// protocols).
package backend

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
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

// Backend is a Git server that dispatches upload-pack and receive-pack
// requests. It implements [http.Handler] for HTTP and provides [Serve]
// and [ServeConn] for stream-based transports.
type Backend struct {
	// Loader resolves repository URLs to storage. If nil,
	// [transport.DefaultLoader] is used.
	Loader transport.Loader

	// ErrorLog is used to log errors. If nil, errors are not logged.
	ErrorLog *log.Logger

	// Prefix is an HTTP path prefix stripped before route matching.
	// Only used by [ServeHTTP].
	Prefix string
}

// New creates a Backend with the given loader.
func New(loader transport.Loader) *Backend {
	if loader == nil {
		loader = transport.DefaultLoader
	}
	return &Backend{
		Loader: loader,
	}
}

// Serve handles a Git pack protocol request. It resolves the repository
// from req.URL, validates the service, and runs the appropriate server
// command (upload-pack or receive-pack).
//
// The caller is responsible for closing r and w. Errors are returned, not
// written to the wire — the caller decides the error format (pkt-line for
// TCP/SSH, HTTP status for HTTP).
func (b *Backend) Serve(ctx context.Context, r io.ReadCloser, w io.WriteCloser, req *Request) error {
	if req.URL == nil {
		return fmt.Errorf("nil request URL")
	}

	loader := b.Loader
	if loader == nil {
		loader = transport.DefaultLoader
	}

	st, err := loader.Load(req.URL)
	if err != nil {
		return err
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
