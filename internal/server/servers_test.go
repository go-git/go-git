package server

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	servergit "github.com/go-git/go-git/v6/internal/server/git"
	serverhttp "github.com/go-git/go-git/v6/internal/server/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
	transportgit "github.com/go-git/go-git/v6/plumbing/transport/git"
	transporthttp "github.com/go-git/go-git/v6/plumbing/transport/http"
)

type serverImplementation struct {
	name         string
	newServer    func(transport.Loader) (GitServer, error)
	newTransport func() transport.Transport
}

var serverImplementations = []serverImplementation{
	{
		name: "git",
		newServer: func(l transport.Loader) (GitServer, error) {
			return servergit.FromLoader(l), nil
		},
		newTransport: func() transport.Transport {
			return transportgit.NewTransport(transportgit.Options{})
		},
	},
	{
		name: "http",
		newServer: func(l transport.Loader) (GitServer, error) {
			return serverhttp.FromLoader(l)
		},
		newTransport: func() transport.Transport {
			return transporthttp.NewTransport(transporthttp.Options{})
		},
	},
}

type ServerSuite struct {
	suite.Suite

	impl serverImplementation
}

func TestServerSuite(t *testing.T) {
	t.Parallel()

	for _, impl := range serverImplementations {
		t.Run(impl.name, func(t *testing.T) {
			t.Parallel()

			suite.Run(t, &ServerSuite{impl: impl})
		})
	}
}

func (s *ServerSuite) TestUploadPack() {
	endpoint := s.startServer(fixtures.Basic().One())

	sess, err := s.impl.newTransport().Handshake(context.Background(), &transport.Request{
		URL:     endpoint.url("/basic.git"),
		Command: transport.UploadPackService,
	})
	s.Require().NoError(err)
	s.T().Cleanup(func() { s.Require().NoError(sess.Close()) })

	refs, err := sess.GetRemoteRefs(context.Background())
	s.Require().NoError(err)
	s.Greater(len(refs), 0, "server should advertise refs")
}

func (s *ServerSuite) TestUnsupportedService() {
	endpoint := s.startServer(fixtures.Basic().One())

	sess, err := s.impl.newTransport().Handshake(context.Background(), &transport.Request{
		URL:     endpoint.url("/basic.git"),
		Command: "git-unsupported-service",
	})
	if err == nil {
		s.Require().NotNil(sess)
		s.T().Cleanup(func() { s.Require().NoError(sess.Close()) })
	}
	s.Require().Error(err)
}

func (s *ServerSuite) TestStartAlreadyStarted() {
	srv, err := s.impl.newServer(nil)
	s.Require().NoError(err)

	endpoint, err := srv.Start()
	s.Require().NoError(err)
	s.NotEmpty(endpoint)
	s.T().Cleanup(func() { s.Require().NoError(srv.Close()) })

	endpoint, err = srv.Start()
	s.Require().Error(err, "Start() after Start() succeeded with endpoint %q", endpoint)
	s.Empty(endpoint)
	s.Contains(err.Error(), "server already started")
}

func (s *ServerSuite) TestStartAfterClose() {
	srv, err := s.impl.newServer(nil)
	s.Require().NoError(err)

	endpoint, err := srv.Start()
	s.Require().NoError(err)
	s.NotEmpty(endpoint)
	s.Require().NoError(srv.Close())

	endpoint, err = srv.Start()
	if err == nil {
		_ = srv.Close()
	}
	s.Require().Error(err, "Start() after Close() succeeded with endpoint %q", endpoint)
	s.Empty(endpoint)
	s.Contains(err.Error(), "server already started")
}

type runningServer struct {
	endpoint string
}

func (s *ServerSuite) startServer(f *fixtures.Fixture) runningServer {
	s.T().Helper()

	srv, err := s.impl.newServer(Loader(s.T(), f))
	s.Require().NoError(err)

	endpoint, err := srv.Start()
	s.Require().NoError(err)
	s.T().Cleanup(func() { s.Require().NoError(srv.Close()) })

	return runningServer{endpoint: endpoint}
}

func (s runningServer) url(path string) *url.URL {
	u, err := url.Parse(s.endpoint + path)
	if err != nil {
		panic(fmt.Sprintf("invalid URL %q: %v", s.endpoint+path, err))
	}
	return u
}
