package transport

import (
	"bytes"
	"context"
	"io"

	"github.com/go-git/go-git/v5/utils/ioutil"
)

type mockCommand struct {
	stdin  bytes.Buffer
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func (c mockCommand) StderrPipe() (io.Reader, error) {
	return &c.stderr, nil
}

func (c mockCommand) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(&c.stdin), nil
}

func (c mockCommand) StdoutPipe() (io.Reader, error) {
	return &c.stdout, nil
}

func (c mockCommand) Start() error {
	return nil
}

func (c mockCommand) Close() error {
	return nil
}

type mockCommander struct {
	stderr string
}

func (c mockCommander) Command(_ context.Context, cmd string, ep *Endpoint, auth AuthMethod, _ ...string) (Command, error) {
	return &mockCommand{
		stderr: *bytes.NewBufferString(c.stderr),
	}, nil
}
