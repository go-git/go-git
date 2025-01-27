package file

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

const bareConfig = `[core]
repositoryformatversion = 0
filemode = true
bare = true`

func prepareRepo(t *testing.T, path string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(path)
	assert.Nil(t, err)

	// git-receive-pack refuses to update refs/heads/master on non-bare repo
	// so we ensure bare repo config.
	config := filepath.Join(path, "config")
	if _, err := os.Stat(config); err == nil {
		f, err := os.OpenFile(config, os.O_TRUNC|os.O_WRONLY, 0)
		assert.Nil(t, err)
		content := strings.NewReader(bareConfig)
		_, err = io.Copy(f, content)
		assert.Nil(t, err)
		assert.Nil(t, f.Close())
	}

	return ep
}
