package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/utils/ioutil"
)

func TestCmdStartEOFSuite(t *testing.T) {
	suite.Run(t, new(CmdStartEOFSuite))
}

type CmdStartEOFSuite struct {
	suite.Suite
}

type mockStartEOFCommand struct {
	stdin         bytes.Buffer
	stdout        bytes.Buffer
	stderr        bytes.Buffer
	sessionOpened bool
}

func (c *mockStartEOFCommand) StderrPipe() (io.Reader, error) {
	return &c.stderr, nil
}

func (c *mockStartEOFCommand) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(&c.stdin), nil
}

func (c *mockStartEOFCommand) StdoutPipe() (io.Reader, error) {
	return &c.stdout, nil
}

func (c *mockStartEOFCommand) Start() error {
	c.sessionOpened = true
	return io.EOF
}

func (c *mockStartEOFCommand) Close() error {
	c.sessionOpened = false
	return nil
}

type mockStartEOFCommander struct {
	mockCmd *mockStartEOFCommand
}

func (c *mockStartEOFCommander) Command(_ context.Context, cmd string, ep *Endpoint, auth AuthMethod, _ ...string) (Command, error) {
	c.mockCmd = &mockStartEOFCommand{}
	return c.mockCmd, nil
}

func (s *CmdStartEOFSuite) TestCmdStartEOFConnectionLeakError() {
	client := NewPackTransport(&mockStartEOFCommander{})
	sess, err := client.NewSession(nil, nil, nil)
	if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), UploadPackService)
	s.ErrorIs(err, io.EOF)
	cmdrInterface := sess.(*PackSession).cmdr
	cmdr := cmdrInterface.(*mockStartEOFCommander)
	s.False(cmdr.mockCmd.sessionOpened)
}
