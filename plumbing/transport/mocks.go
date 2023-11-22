package transport

import (
	"bytes"
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
	panic("not implemented")
}

type mockCommander struct {
	stderr string
}

func (c mockCommander) Command(cmd string, ep *Endpoint, auth AuthMethod) (Command, error) {
	return &mockCommand{
		stderr: *bytes.NewBufferString(c.stderr),
	}, nil
}
