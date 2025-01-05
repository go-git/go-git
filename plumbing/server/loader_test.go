package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/suite"
)

type loaderSuiteRepo struct {
	bare bool

	path string
}

type LoaderSuite struct {
	suite.Suite
	Repos map[string]loaderSuiteRepo
}

func TestLoaderSuite(t *testing.T) {
	suite.Run(t,
		&LoaderSuite{
			Repos: map[string]loaderSuiteRepo{
				"repo": {path: "repo.git"},
				"bare": {path: "bare.git", bare: true},
			},
		},
	)
}

func (s *LoaderSuite) SetupSuite() {
	if err := exec.Command("git", "--version").Run(); err != nil {
		s.T().Skip("git command not found")
	}

	dir, err := os.MkdirTemp("", "")
	s.NoError(err)

	for key, repo := range s.Repos {
		repo.path = filepath.Join(dir, repo.path)
		if repo.bare {
			s.Nil(exec.Command("git", "init", "--bare", repo.path).Run())
		} else {
			s.Nil(exec.Command("git", "init", repo.path).Run())
		}
		s.Repos[key] = repo
	}

}

func (s *LoaderSuite) endpoint(url string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(url)
	s.NoError(err)
	return ep
}

func (s *LoaderSuite) TestLoadNonExistent() {
	sto, err := DefaultLoader.Load(s.endpoint("does-not-exist"))
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(sto)
}

func (s *LoaderSuite) TestLoadNonExistentIgnoreHost() {
	sto, err := DefaultLoader.Load(s.endpoint("https://github.com/does-not-exist"))
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(sto)
}

func (s *LoaderSuite) TestLoad() {
	sto, err := DefaultLoader.Load(s.endpoint(s.Repos["repo"].path))
	s.NoError(err)
	s.NotNil(sto)
}

func (s *LoaderSuite) TestLoadBare() {
	sto, err := DefaultLoader.Load(s.endpoint(s.Repos["bare"].path))
	s.NoError(err)
	s.NotNil(sto)
}

func (s *LoaderSuite) TestMapLoader() {
	ep, err := transport.NewEndpoint("file://test")
	sto := memory.NewStorage()
	s.NoError(err)

	loader := MapLoader{ep.String(): sto}

	ep, err = transport.NewEndpoint("file://test")
	s.NoError(err)

	loaderSto, err := loader.Load(ep)
	s.NoError(err)
	s.Equal(loaderSto, sto)
}
