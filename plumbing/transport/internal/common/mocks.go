package common

import (
	"bytes"
	"io"

	gogitioutil "github.com/go-git/go-git/v5/utils/ioutil"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

type MockCommand struct {
	stdin  bytes.Buffer
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func (c MockCommand) StderrPipe() (io.Reader, error) {
	return &c.stderr, nil
}

func (c MockCommand) StdinPipe() (io.WriteCloser, error) {
	return gogitioutil.WriteNopCloser(&c.stdin), nil
}

func (c MockCommand) StdoutPipe() (io.Reader, error) {
	return &c.stdout, nil
}

func (c MockCommand) Start() error {
	return nil
}

func (c MockCommand) Wait() error {
	panic("not implemented")
}

// Deprecated
func (c MockCommand) Close() error {
	return c.Wait()
}

type MockCommander struct {
	stderr string
}

func (c MockCommander) Command(cmd string, ep *transport.Endpoint, auth transport.AuthMethod) (Command, error) {
	return &MockCommand{
		stderr: *bytes.NewBufferString(c.stderr),
	}, nil
}
