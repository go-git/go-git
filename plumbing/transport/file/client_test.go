package file

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
}

type testLoader struct {
	repos map[*transport.Endpoint]storage.Storer
}

func (l *testLoader) Load(ep *transport.Endpoint) (storage.Storer, error) {
	repo, ok := l.repos[ep]
	if !ok {
		return nil, transport.ErrRepositoryNotFound
	}
	return repo, nil
}

func (s *ClientSuite) TestCommand() {
	ep, err := transport.NewEndpoint(filepath.Join("fake", "repo"))
	s.Nil(err)
	runner := &runner{
		loader: &testLoader{
			repos: map[*transport.Endpoint]storage.Storer{
				ep: memory.NewStorage(),
			},
		},
	}
	var emptyAuth transport.AuthMethod
	_, err = runner.Command(context.TODO(), "git-receive-pack", ep, emptyAuth)
	s.Nil(err)

	// Make sure we get an error for one that doesn't exist.
	_, err = runner.Command(context.TODO(), "git-fake-command", ep, emptyAuth)
	s.NotNil(err)
}
