// Package file implements the file transport protocol.
package file

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/internal/repository"
	"github.com/go-git/go-git/v5/plumbing/server"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"golang.org/x/sys/execabs"
)

func init() {
	transport.Register("file", DefaultClient)
}

// DefaultClient is the default local client.
var DefaultClient = NewClient(
	transport.UploadPackServiceName,
	transport.ReceivePackServiceName,
)

type runner struct {
	UploadPackBin  string
	ReceivePackBin string
}

// NewClient returns a new local client using the given git-upload-pack and
// git-receive-pack binaries.
func NewClient(uploadPackBin, receivePackBin string) transport.Transport {
	return transport.NewClient(&runner{
		UploadPackBin:  uploadPackBin,
		ReceivePackBin: receivePackBin,
	})
}

func prefixExecPath(cmd string) (string, error) {
	// Use `git --exec-path` to find the exec path.
	execCmd := execabs.Command("git", "--exec-path")

	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stdoutBuf := bufio.NewReader(stdout)

	err = execCmd.Start()
	if err != nil {
		return "", err
	}

	execPathBytes, isPrefix, err := stdoutBuf.ReadLine()
	if err != nil {
		return "", err
	}
	if isPrefix {
		return "", errors.New("couldn't read exec-path line all at once")
	}

	err = execCmd.Wait()
	if err != nil {
		return "", err
	}
	execPath := string(execPathBytes)
	execPath = strings.TrimSpace(execPath)
	cmd = filepath.Join(execPath, cmd)

	// Make sure it actually exists.
	_, err = execabs.LookPath(cmd)
	if err != nil {
		return "", err
	}
	return cmd, nil
}

func (r *runner) Command(ctx context.Context, cmd string, ep *transport.Endpoint, auth transport.AuthMethod, params ...string) (transport.Command, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	return &command{
		ctx:     ctx,
		cancel:  cancel,
		ep:      ep,
		service: cmd,
		errc:    make(chan error, 1),
	}, nil
}

type command struct {
	ctx     context.Context
	cancel  context.CancelFunc
	ep      *transport.Endpoint
	service string

	stdin  io.Reader
	stdout io.WriteCloser
	stderr io.Writer

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
		go func() {
			c.errc <- server.UploadPack(
				c.ctx,
				st,
				c.stdin,
				c.stdout,
				nil,
			)
		}()
		return nil
	case transport.ReceivePackServiceName:
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
func (c *command) Close() error {
	if c.closed {
		return nil
	}

	closeDiscriptors(c.childIOFiles)
	closeDiscriptors(c.parentIOFiles)

	return nil
}

func closeDiscriptors(fds []io.Closer) {
	for _, fd := range fds {
		fd.Close()
	}
}
