package git

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// DefaultBackend is the default Git backend.
var DefaultBackend = NewBackend(transport.DefaultLoader, &BackendOptions{
	UploadPack:  true,
	ReceivePack: false,
	// UploadArchive: true,
	ErrorLog: log.Default(),
})

// BackendOptions contains options for the [NewBackend].
type BackendOptions struct {
	UploadPack  bool
	ReceivePack bool
	// UploadArchive bool

	ErrorLog *log.Logger
}

// GitProtoFunc is a function that handles Git protocol requests.
type GitProtoFunc = func(ctx context.Context, c net.Conn)

// NewBackend represents a Git transport handler.
func NewBackend(loader transport.Loader, opts *BackendOptions) GitProtoFunc {
	if loader == nil {
		loader = transport.DefaultLoader
	}
	return func(ctx context.Context, c net.Conn) {
		r := ioutil.NewContextReader(ctx, c)
		wc := ioutil.NewContextWriteCloser(ctx, c)

		var req packp.GitProtoRequest
		if err := req.Decode(r); err != nil {
			logf(opts.ErrorLog, "error decoding request: %v", err)
			return
		}

		svc := transport.Service(req.RequestCommand)
		if (svc != transport.UploadPackService && svc != transport.ReceivePackService) ||
			(svc == transport.UploadPackService && !opts.UploadPack) ||
			(svc == transport.ReceivePackService && !opts.ReceivePack) {
			renderError(opts.ErrorLog, wc, transport.ErrUnsupportedService)
			return
		}

		host := req.Host
		if host == "" {
			host = "localhost"
		}

		url, err := url.JoinPath(fmt.Sprintf("git://%s", host), req.Pathname)
		if err != nil {
			renderError(opts.ErrorLog, wc, transport.ErrRepositoryNotFound)
			return
		}

		ep, err := transport.NewEndpoint(url)
		if err != nil {
			// XXX: Should we use a more descriptive error?
			renderError(opts.ErrorLog, wc, transport.ErrRepositoryNotFound)
			return
		}

		st, err := loader.Load(ep)
		if err != nil {
			renderError(opts.ErrorLog, wc, err)
			return
		}

		version := strings.Join(req.ExtraParams, ":")
		switch svc {
		case transport.UploadPackService:
			err = transport.UploadPack(ctx, st,
				io.NopCloser(r), ioutil.WriteNopCloser(wc),
				&transport.UploadPackOptions{
					GitProtocol: version,
				})
		case transport.ReceivePackService:
			err = transport.ReceivePack(ctx, st,
				io.NopCloser(r), ioutil.WriteNopCloser(wc),
				&transport.ReceivePackOptions{
					GitProtocol: version,
				})
		}

		if err != nil {
			renderError(opts.ErrorLog, wc, transport.ErrRepositoryNotFound)
			logf(opts.ErrorLog, "error handling request: %v", err)
			return
		}

		if err := c.Close(); err != nil {
			logf(opts.ErrorLog, "error closing connection: %v", err)
		}
	}
}

func logf(logger *log.Logger, format string, args ...interface{}) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

func renderError(logger *log.Logger, rw io.WriteCloser, err error) {
	if _, err := pktline.WriteError(rw, err); err != nil {
		logf(logger, "error writing error: %v", err)
		rw.Close() //nolint:errcheck
		return
	}
	if err := rw.Close(); err != nil {
		logf(logger, "error closing writer: %v", err)
	}
}
