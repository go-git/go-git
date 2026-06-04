// Package file implements the file transport for the new transport API.
package file

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
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

func defaultUploadArchive(ctx context.Context, st storage.Storer, r io.ReadCloser, w io.WriteCloser, _ string) error {
	return transport.UploadArchive(ctx, st, r, w, nil)
}

// Options configures the file transport.
type Options struct {
	// Loader resolves URLs to storage.Storer instances. If nil,
	// transport.DefaultLoader is used.
	Loader transport.Loader
}

// Transport implements the file:// transport protocol.
type Transport struct {
	loader        transport.Loader
	uploadPack    ServerFunc
	receivePack   ServerFunc
	uploadArchive ServerFunc
}

// NewTransport creates a file transport with the given options.
func NewTransport(opts Options) *Transport {
	loader := opts.Loader
	if loader == nil {
		loader = transport.DefaultLoader
	}
	return &Transport{
		loader:        loader,
		uploadPack:    defaultUploadPack,
		receivePack:   defaultReceivePack,
		uploadArchive: defaultUploadArchive,
	}
}

// Connect opens a raw connection to the file transport process.
func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	sr, pw, closeAll, err := t.connect(ctx, req)
	if err != nil {
		return nil, err
	}
	conn := &fileConn{r: sr, w: pw, close: closeAll}
	setupLeakCheck(conn)
	return conn, nil
}

func (t *Transport) connect(ctx context.Context, req *transport.Request) (io.Reader, *io.PipeWriter, func() error, error) {
	var serverFn ServerFunc
	switch req.Command {
	case transport.UploadPackService:
		serverFn = t.uploadPack
	case transport.ReceivePackService:
		serverFn = t.receivePack
	case transport.UploadArchiveService:
		serverFn = t.uploadArchive
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

	done := make(chan struct{})

	closeAll := func() error {
		_ = pw.Close()
		closeErr := sr.Close()
		<-done // Wait for server goroutine to finish
		if closer, ok := st.(io.Closer); ok {
			if err := closer.Close(); err != nil && closeErr == nil {
				closeErr = err
			}
		}
		return closeErr
	}

	go func() {
		defer close(done)
		err := serverFn(ctx, st, io.NopCloser(pr), sw, gitProtocol)
		_ = sw.CloseWithError(err)
		_ = pr.Close()
	}()

	return sr, pw, closeAll, nil
}

// fileConn implements transport.Conn over in-process pipes.
type fileConn struct {
	r      io.Reader
	w      io.WriteCloser
	close  func() error
	closed bool
}

var _ transport.Conn = (*fileConn)(nil)

func (c *fileConn) Reader() io.Reader      { return c.r }
func (c *fileConn) Writer() io.WriteCloser { return c.w }
func (c *fileConn) Close() error {
	c.closed = true
	return c.close()
}
