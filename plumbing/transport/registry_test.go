package transport_test

import (
	"net/http"

	"github.com/go-git/go-git/v5/plumbing/protocol"
	_ "github.com/go-git/go-git/v5/plumbing/transport/ssh" // ssh transport
	"github.com/go-git/go-git/v5/storage"

	"github.com/go-git/go-git/v5/plumbing/transport"
	. "gopkg.in/check.v1"
)

type ClientSuite struct{}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) TestNewClientSSH(c *C) {
	e, err := transport.NewEndpoint("ssh://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	output, err := transport.Get(e.Protocol)
	c.Assert(err, IsNil)
	c.Assert(output, NotNil)
}

func (s *ClientSuite) TestNewClientUnknown(c *C) {
	e, err := transport.NewEndpoint("unknown://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = transport.Get(e.Protocol)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestNewClientNil(c *C) {
	transport.Register("newscheme", nil)
	e, err := transport.NewEndpoint("newscheme://github.com/src-d/go-git")
	c.Assert(err, IsNil)

	_, err = transport.Get(e.Protocol)
	c.Assert(err, NotNil)
}

func (s *ClientSuite) TestInstallProtocol(c *C) {
	transport.Register("newscheme", &dummyClient{})
	p, err := transport.Get("newscheme")
	c.Assert(err, IsNil)
	c.Assert(p, NotNil)
}

func (s *ClientSuite) TestInstallProtocolNilValue(c *C) {
	transport.Register("newscheme", &dummyClient{})
	transport.Unregister("newscheme")

	_, err := transport.Get("newscheme")
	c.Assert(err, NotNil)
}

type dummyClient struct {
	*http.Client
}

var _ transport.Transport = &dummyClient{}

func (*dummyClient) NewSession(storage.Storer, *transport.Endpoint, transport.AuthMethod) (transport.Session, error) {
	return nil, nil
}

// SupportedProtocols implements transport.Transport.
func (d *dummyClient) SupportedProtocols() []protocol.Version {
	return []protocol.Version{}
}
