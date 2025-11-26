package transport

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage"
)

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(RegistrySuite))
}

type RegistrySuite struct {
	suite.Suite
}

func (s *RegistrySuite) TestNewClientSSH() {
	Register("ssh", &dummyClient{})
	e, err := NewEndpoint("ssh://github.com/src-d/go-git")
	s.NoError(err)

	output, err := Get(e.Scheme)
	s.NoError(err)
	s.NotNil(output)
}

func (s *RegistrySuite) TestNewClientUnknown() {
	e, err := NewEndpoint("unknown://github.com/src-d/go-git")
	s.NoError(err)

	_, err = Get(e.Scheme)
	s.Error(err)
}

func (s *RegistrySuite) TestNewClientNil() {
	Register("newscheme", nil)
	e, err := NewEndpoint("newscheme://github.com/src-d/go-git")
	s.NoError(err)

	_, err = Get(e.Scheme)
	s.Error(err)
}

func (s *RegistrySuite) TestInstallProtocol() {
	Register("newscheme", &dummyClient{})
	p, err := Get("newscheme")
	s.NoError(err)
	s.NotNil(p)
}

func (s *RegistrySuite) TestInstallProtocolNilValue() {
	Register("newscheme", &dummyClient{})
	Unregister("newscheme")

	_, err := Get("newscheme")
	s.Error(err)
}

type dummyClient struct {
	*http.Client
}

func (*dummyClient) NewSession(storage.Storer, *Endpoint, AuthMethod) (
	Session, error,
) {
	return nil, nil
}

func (*dummyClient) SupportedProtocols() []protocol.Version {
	return []protocol.Version{}
}
