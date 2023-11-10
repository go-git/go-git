// Package file implements the file transport protocol.
package file

import (
	"errors"
	"io"
	"os"

	"github.com/go-git/go-git/v5/internal/repository"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/internal/common"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

// DefaultClient is the default local client.
var DefaultClient = NewClient()

// NewClient returns a new local client using the given git-upload-pack and
// git-receive-pack binaries.
func NewClient() transport.Transport {
	return common.NewClient(&runner{})
}

type runner struct{}

func NewRunner() *runner {
	return &runner{}
}

func (r *runner) Command(cmd string, ep *transport.Endpoint, auth transport.AuthMethod,
) (common.Command, error) {
	switch cmd {
	case transport.UploadPackServiceName, transport.ReceivePackServiceName:
		return &command{cmd: cmd, path: ep.Path, errc: make(chan error, 1)}, nil
	}

	return nil, errors.New("unknown command")
}

type command struct {
	cmd    string
	path   string
	stdin  io.Reader
	stdout io.WriteCloser
	stderr io.Writer

	childIOFiles  []*os.File
	parentIOFiles []*os.File

	started bool
	closed  bool
	errc    chan error
}

func (c *command) Start() error {
	st, _, err := repository.PlainOpen(c.path, true, false)
	if err != nil {
		return err
	}

	defer func() {
		if !c.started {
			closeDiscriptors(c.parentIOFiles)
			c.errc <- err
		}
	}()

	cmd := server.ServerCommand{
		Stdin:  c.stdin,
		Stdout: c.stdout,
		Stderr: c.stderr,
	}

	switch c.cmd {
	case transport.UploadPackServiceName:
		go func() {
			err := server.ServeUploadPack(cmd, st)
			c.errc <- err
		}()
		c.started = true
	case transport.ReceivePackServiceName:
		go func() {
			c.errc <- server.ServeReceivePack(cmd, st)
		}()
		c.started = true
	default:
		return errors.New("unknown command")
	}

	return nil
}

func (c *command) StdinPipe() (io.WriteCloser, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	c.stdin = pr
	c.childIOFiles = append(c.childIOFiles, pr)
	c.parentIOFiles = append(c.parentIOFiles, pw)

	return pw, nil
}

func (c *command) StdoutPipe() (io.Reader, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	c.stdout = pw
	c.childIOFiles = append(c.childIOFiles, pw)
	c.parentIOFiles = append(c.parentIOFiles, pr)

	return pr, nil
}

func (c *command) StderrPipe() (io.Reader, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	c.stderr = pw
	c.childIOFiles = append(c.childIOFiles, pw)
	c.parentIOFiles = append(c.parentIOFiles, pr)

	return pr, nil
}

func (c *command) Kill() error {
	return c.Close()
}

// Close waits for the command to exit.
func (c *command) Close() error {
	if c.closed {
		return nil
	}
	defer func() {
		closeDiscriptors(c.childIOFiles)
		closeDiscriptors(c.parentIOFiles)
	}()
	select {
	case err := <-c.errc:
		c.closed = true
		return err
	}
}

func closeDiscriptors(fds []*os.File) {
	for _, fd := range fds {
		fd.Close()
	}
}
