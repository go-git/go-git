package git

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// Backend represents a Git transport server handler that can handle
// git-upload-pack, git-receive-pack, and git-upload-archive requests over TCP.
type Backend struct {
	// Loader is used to load repositories. It uses [transport.DefaultLoader]
	// when nil.
	Loader transport.Loader
	// UploadPack indicates whether the handler should handle
	// git-upload-pack requests.
	UploadPack bool
	// ReceivePack indicates whether the handler should handle
	// git-receive-pack requests.
	ReceivePack bool
	// ArchivePack indicates whether the handler should handle
	// git-upload-archive requests.
	// ArchivePack bool // TODO: Implement git-upload-archive support
}

// NewBackend creates a new [Backend] for the given loader. It defaults to
// enabling both git-upload-pack and git-upload-archive but not
// git-receive-pack.
func NewBackend(loader transport.Loader) *Backend {
	return &Backend{
		Loader:      loader,
		UploadPack:  true,
		ReceivePack: false,
		// ArchivePack: true, // TODO: Implement git-upload-archive support
	}
}

// ServeTCP implements the [Handler] interface for the [Backend].
// TODO: Support idle timeout based on the context. Something like
// context.WithIdleTimeout where it resets the timer on each read/write
// operation.
func (b *Backend) ServeTCP(ctx context.Context, c io.ReadWriteCloser, req *packp.GitProtoRequest) {
	loader := b.Loader
	if loader == nil {
		loader = transport.DefaultLoader
	}

	r := ioutil.NewContextReader(ctx, c)
	wc := ioutil.NewContextWriteCloser(ctx, c)

	// Ensure we close the connection when we're done.
	defer c.Close() //nolint:errcheck

	svc := transport.Service(req.RequestCommand)
	switch {
	case svc == transport.UploadPackService && b.UploadPack,
		svc == transport.ReceivePackService && b.ReceivePack:
		// TODO: Support git-upload-archive
	default:
		renderError(wc, transport.ErrUnsupportedService) //nolint:errcheck
		return
	}

	host := req.Host
	if host == "" {
		host = "localhost"
	}

	url, err := url.JoinPath(fmt.Sprintf("git://%s", host), req.Pathname)
	if err != nil {
		renderError(wc, transport.NewRepositoryNotFoundError(err)) //nolint:errcheck
		return
	}

	ep, err := transport.NewEndpoint(url)
	if err != nil {
		renderError(wc, transport.NewRepositoryNotFoundError(err)) //nolint:errcheck
		return
	}

	st, err := loader.Load(ep)
	if err != nil {
		renderError(wc, err) //nolint:errcheck
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
		renderError(wc, transport.NewRepositoryNotFoundError(err)) //nolint:errcheck
		return
	}
}

func renderError(rw io.WriteCloser, err error) error {
	if _, err := pktline.WriteError(rw, err); err != nil {
		rw.Close() //nolint:errcheck
		return err
	}
	return rw.Close()
}
