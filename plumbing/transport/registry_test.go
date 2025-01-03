package transport_test

import (
	"net/http"
	"testing"

	_ "github.com/go-git/go-git/v5/plumbing/transport/ssh" // ssh transport
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestSuiteCommon(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
}

func (s *ClientSuite) TestNewClientSSH() {
	e, err := transport.NewEndpoint("ssh://github.com/src-d/go-git")
	s.Require().NoError(err)

	output, err := transport.Get(e.Protocol)
	s.Require().NoError(err)
	s.NotNil(output)
}

func (s *ClientSuite) TestNewClientUnknown() {
	e, err := transport.NewEndpoint("unknown://github.com/src-d/go-git")
	s.Require().NoError(err)

	_, err = transport.Get(e.Protocol)
	s.Error(err)
}

func (s *ClientSuite) TestNewClientNil() {
	transport.Register("newscheme", nil)
	e, err := transport.NewEndpoint("newscheme://github.com/src-d/go-git")
	s.Require().NoError(err)

	_, err = transport.Get(e.Protocol)
	s.Error(err)
}

func (s *ClientSuite) TestInstallProtocol() {
	transport.Register("newscheme", &dummyClient{})
	p, err := transport.Get("newscheme")
	s.Require().NoError(err)
	s.NotNil(p)
}

func (s *ClientSuite) TestInstallProtocolNilValue() {
	transport.Register("newscheme", &dummyClient{})
	transport.Unregister("newscheme")

	_, err := transport.Get("newscheme")
	s.Error(err)
}

type dummyClient struct {
	*http.Client
}

func (*dummyClient) NewUploadPackSession(*transport.Endpoint, transport.AuthMethod) (
	transport.UploadPackSession, error,
) {
	return nil, nil
}

func (*dummyClient) NewReceivePackSession(*transport.Endpoint, transport.AuthMethod) (
	transport.ReceivePackSession, error,
) {
	return nil, nil
}
