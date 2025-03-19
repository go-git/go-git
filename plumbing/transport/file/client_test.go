package file

import (
	"context"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
	helper CommonSuiteHelper
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
	ep := s.helper.newEndpoint(s.T(), filepath.Join("fake", "repo"))
	runner := &runner{
		loader: &testLoader{
			repos: map[*transport.Endpoint]storage.Storer{
				ep: memory.NewStorage(),
			},
		},
	}
	var emptyAuth transport.AuthMethod
	_, err := runner.Command(context.TODO(), "git-receive-pack", ep, emptyAuth)
	s.NoError(err)

	// Make sure we get an error for one that doesn't exist.
	_, err = runner.Command(context.TODO(), "git-fake-command", ep, emptyAuth)
	s.Error(err)
}

type CommonSuiteHelper struct{}

func (h *CommonSuiteHelper) newEndpoint(t *testing.T, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(name)
	assert.NoError(t, err)

	return ep
}

func (h *CommonSuiteHelper) prepareRepository(t *testing.T, f *fixtures.Fixture) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	assert.NoError(t, err)

	return h.newEndpoint(t, fs.Root())
}
