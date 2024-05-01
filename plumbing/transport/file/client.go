// Package file implements the file transport protocol.
package file

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/internal/repository"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/server"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func init() {
	transport.Register("file", DefaultTransport)
}

// DefaultTransport is the default local client.
var DefaultTransport = NewTransport()

type runner struct{}

// NewTransport returns a new file transport that users go-git built-in server
// implementation to serve repositories.
func NewTransport() transport.Transport {
	return transport.NewTransport(&runner{})
}

func (r *runner) Command(ctx context.Context, cmd string, ep *transport.Endpoint, auth transport.AuthMethod, params ...string) (transport.Command, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	var gitProtocol string
	for _, param := range params {
		if strings.HasPrefix("version=", param) {
			if v, _ := strconv.Atoi(param[8:]); v > 0 {
				gitProtocol += param
			}
		}
	}

	return &command{
		ctx:         ctx,
		cancel:      cancel,
		ep:          ep,
		service:     cmd,
		errc:        make(chan error, 1),
		gitProtocol: gitProtocol,
	}, nil
}

type command struct {
	ctx         context.Context
	cancel      context.CancelFunc
	ep          *transport.Endpoint
	service     string
	gitProtocol string

	stdin  io.ReadCloser
	stdinW *io.PipeWriter
	stdout io.WriteCloser
	stderr io.WriteCloser

	childIOFiles  []io.Closer
	parentIOFiles []io.Closer

	closed bool
	errc   chan error
}

func (c *command) Start() error {
	st, _, err := repository.PlainOpen(c.ep.Path, true, false)
	if err != nil {
		return fmt.Errorf("failed to load repository: %w", err)
	}

	switch c.service {
	case transport.UploadPackServiceName:
		opts := &server.UploadPackOptions{
			GitProtocol: c.gitProtocol,
		}
		go func() {
			c.errc <- server.UploadPack(
				c.ctx,
				st,
				io.NopCloser(c.stdin),
				c.stdout,
				opts,
			)
		}()
		return nil
	case transport.ReceivePackServiceName:
		opts := &server.ReceivePackOptions{
			GitProtocol: c.gitProtocol,
		}
		go func() {
			c.errc <- server.ReceivePack(
				c.ctx,
				st,
				io.NopCloser(c.stdin),
				c.stdout,
				opts,
			)
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

	// XXX: Write a flush to stdin to signal the end of the request when the
	// client has everything it asked for.
	err = c.stdinW.CloseWithError(pktline.WriteFlush(c.stdinW))

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
