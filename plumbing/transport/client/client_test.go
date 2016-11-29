package client

import (
	"fmt"
	"net/http"
	"testing"

	"gopkg.in/src-d/go-git.v4/plumbing/transport"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct{}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) TestNewClientHTTP(c *C) {
	e, err := transport.NewEndpoint("http://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := NewClient(e)
	c.Assert(err, IsNil)
	c.Assert(typeAsString(output), Equals, "*http.client")

	e, err = transport.NewEndpoint("https://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err = NewClient(e)
	c.Assert(err, IsNil)
	c.Assert(typeAsString(output), Equals, "*http.client")
}

func (s *ClientSuite) TestNewClientSSH(c *C) {
	e, err := transport.NewEndpoint("ssh://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := NewClient(e)
	c.Assert(err, IsNil)
	c.Assert(output, NotNil)
}

func (s *ClientSuite) TestNewClientUnknown(c *C) {
	e, err := transport.NewEndpoint("unknown://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = NewClient(e)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestInstallProtocol(c *C) {
	InstallProtocol("newscheme", &dummyClient{})
	c.Assert(Protocols["newscheme"], NotNil)
}

type dummyClient struct {
	*http.Client
}

func (*dummyClient) NewFetchPackSession(transport.Endpoint) (
	transport.FetchPackSession, error) {
	return nil, nil
}

func (*dummyClient) NewSendPackSession(transport.Endpoint) (
	transport.SendPackSession, error) {
	return nil, nil
}

func typeAsString(v interface{}) string {
	return fmt.Sprintf("%T", v)
}
