// Package file implements the file transport protocol.
package file

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

func init() {
	transport.Register("file", DefaultTransport)
}

// DefaultTransport is the default local client.
var DefaultTransport = NewTransport(nil)

type runner struct {
	loader transport.Loader
}

// NewTransport returns a new file transport that users go-git built-in server
// implementation to serve repositories.
func NewTransport(loader transport.Loader) transport.Transport {
	if loader == nil {
		loader = transport.DefaultLoader
	}
	return transport.NewPackTransport(&runner{loader})
}

func (r *runner) Command(ctx context.Context, cmd string, ep *transport.Endpoint, auth transport.AuthMethod, params ...string) (transport.Command, error) {
	switch transport.Service(cmd) {
	case transport.UploadPackService, transport.ReceivePackService:
		// do nothing
	default:
		return nil, transport.ErrUnsupportedService
	}
	gitProtocol := strings.Join(params, ":")

	return &command{
		ctx:         ctx,
		loader:      r.loader,
		ep:          ep,
		service:     cmd,
		errc:        make(chan error, 1),
		gitProtocol: gitProtocol,
	}, nil
}

type command struct {
	ctx         context.Context
	ep          *transport.Endpoint
	loader      transport.Loader
	service     string
	gitProtocol string

	stdin  *io.PipeReader
	stdinW *io.PipeWriter
	stdout *io.PipeWriter
	stderr *io.PipeWriter

	childIOFiles  []io.Closer
	parentIOFiles []io.Closer

	closed bool
	errc   chan error
}

func (c *command) Start() error {
	st, err := c.loader.Load(c.ep)
	if err != nil {
		return err
	}

	switch transport.Service(c.service) {
	case transport.UploadPackService:
		opts := &transport.UploadPackOptions{
			GitProtocol: c.gitProtocol,
		}
		go func() {
			if err := transport.UploadPack(
				c.ctx,
				st,
				io.NopCloser(c.stdin),
				c.stdout,
				opts,
			); err != nil {
				// Write the error to the stderr pipe and close the command.
				_, _ = fmt.Fprintln(c.stderr, err)
				_ = c.Close()
			}
		}()
		return nil
	case transport.ReceivePackService:
		opts := &transport.ReceivePackOptions{
			GitProtocol: c.gitProtocol,
		}
		go func() {
			if err := transport.ReceivePack(
				c.ctx,
				st,
				io.NopCloser(c.stdin),
				c.stdout,
				opts,
			); err != nil {
				_, _ = fmt.Fprintln(c.stderr, err)
				_ = c.Close()
			}
		}()
		return nil
	}
	return fmt.Errorf("unsupported service: %s", c.service)
}

func (c *command) StderrPipe() (io.Reader, error) {
	pr, pw := io.Pipe()

	c.stderr = pw
	c.childIOFiles = append(c.childIOFiles, pw)
	c.parentIOFiles = append(c.parentIOFiles, pr)

	return pr, nil
}

func (c *command) StdinPipe() (io.WriteCloser, error) {
	pr, pw := io.Pipe()

	c.stdin = pr
	c.stdinW = pw
	c.childIOFiles = append(c.childIOFiles, pr)
	c.parentIOFiles = append(c.parentIOFiles, pw)

	return pw, nil
}

func (c *command) StdoutPipe() (io.Reader, error) {
	pr, pw := io.Pipe()

	c.stdout = pw
	c.childIOFiles = append(c.childIOFiles, pw)
	c.parentIOFiles = append(c.parentIOFiles, pr)

	return pr, nil
}

// Close waits for the command to exit.
func (c *command) Close() (err error) {
	if c.closed {
		return nil
	}

	closeDiscriptors(c.childIOFiles)
	closeDiscriptors(c.parentIOFiles)
	c.closed = true

	return
}

func closeDiscriptors(fds []io.Closer) {
	for _, fd := range fds {
		fd.Close()
	}
}
