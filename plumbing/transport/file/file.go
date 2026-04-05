// Package file implements the file transport for the new transport API.
package file

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/storage"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// ServerFunc is a function that runs a git server-side command over pipes.
type ServerFunc func(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, gitProtocol string) error

func defaultUploadPack(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, gitProtocol string) error {
	return transport.UploadPack(ctx, st, r, w, &transport.UploadPackRequest{
		GitProtocol:          gitProtocol,
		SkipDeltaCompression: true,
	})
}

func defaultReceivePack(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, gitProtocol string) error {
	return transport.ReceivePack(ctx, st, r, w, &transport.ReceivePackRequest{
		GitProtocol: gitProtocol,
	})
}

// Options configures the file transport.
type Options struct {
	// Loader resolves URLs to storage.Storer instances. If nil,
	// transport.DefaultLoader is used.
	Loader transport.Loader
}

// Transport implements the file:// transport protocol.
type Transport struct {
	loader      transport.Loader
	uploadPack  ServerFunc
	receivePack ServerFunc
}

// NewTransport creates a file transport with the given options.
func NewTransport(opts Options) *Transport {
	loader := opts.Loader
	if loader == nil {
		loader = transport.DefaultLoader
	}
	return &Transport{
		loader:      loader,
		uploadPack:  defaultUploadPack,
		receivePack: defaultReceivePack,
	}
}

func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	sr, pw, closeAll, err := t.connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewConn(sr, pw, closeAll), nil
}

func (t *Transport) connect(ctx context.Context, req *transport.Request) (io.Reader, *io.PipeWriter, func() error, error) {
	var serverFn ServerFunc
	switch req.Command {
	case "git-upload-pack":
		serverFn = t.uploadPack
	case "git-receive-pack":
		serverFn = t.receivePack
	default:
		return nil, nil, nil, fmt.Errorf("%w: %s", transport.ErrCommandUnsupported, req.Command)
	}

	st, err := t.loader.Load(req.URL)
	if err != nil {
		return nil, nil, nil, err
	}

	gitProtocol := transport.GitProtocolEnv(req.Protocol)

	pr, pw := io.Pipe()
	sr, sw := io.Pipe()

	closeAll := func() error { _ = pw.Close(); return sr.Close() }

	go func() {
		err := serverFn(ctx, st, io.NopCloser(pr), sw, gitProtocol)
		_ = sw.CloseWithError(err)
		_ = pr.Close()
	}()

	return sr, pw, closeAll, nil
}

type streamConn struct {
	io.Reader
	io.Writer
	closeFunc func() error
}

func (c *streamConn) Close() error {
	return c.closeFunc()
}
