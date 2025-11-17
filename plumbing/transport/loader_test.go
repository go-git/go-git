package transport

import (
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"
)

type loaderSuiteRepo struct {
	bare bool

	path string
}

func TestLoaderSuite(t *testing.T) {
	suite.Run(t, new(LoaderSuite))
}

type LoaderSuite struct {
	suite.Suite
	loader Loader
	repos  map[string]loaderSuiteRepo
}

func (s *LoaderSuite) SetupSuite() {
	s.repos = map[string]loaderSuiteRepo{
		"repo": {path: "repo.git"},
		"bare": {path: "bare.git", bare: true},
	}
	dir := s.T().TempDir()
	s.loader = NewFilesystemLoader(osfs.New(dir), false)
	for key, repo := range s.repos {
		repo.path = filepath.Join(dir, repo.path)
		st := filesystem.NewStorage(osfs.New(repo.path), nil)
		err := st.Init()
		s.NoError(err)
		cfg, err := st.Config()
		s.NoError(err)
		if repo.bare {
			cfg.Core.IsBare = repo.bare
		}
		err = st.SetConfig(cfg)
		s.NoError(err)
		s.repos[key] = repo
	}
}

func (s *LoaderSuite) Load(ep *Endpoint) (storage.Storer, error) {
	_, ok := s.repos[ep.Path]
	if !ok {
		return nil, ErrRepositoryNotFound
	}
	return memory.NewStorage(), nil
}

func (s *LoaderSuite) endpoint(url string) *Endpoint {
	ep, err := NewEndpoint(url)
	s.Nil(err)
	return ep
}

func (s *LoaderSuite) TestLoadNonExistent() {
	sto, err := s.loader.Load(s.endpoint("does-not-exist"))
	s.ErrorIs(err, ErrRepositoryNotFound)
	s.Nil(sto)
}

func (s *LoaderSuite) TestLoadNonExistentIgnoreHost() {
	sto, err := s.loader.Load(s.endpoint("https://github.com/does-not-exist"))
	s.ErrorIs(err, ErrRepositoryNotFound)
	s.Nil(sto)
}

func (s *LoaderSuite) TestLoad() {
	sto, err := s.loader.Load(s.endpoint("repo"))
	s.Nil(err)
	s.NotNil(sto)
}

func (s *LoaderSuite) TestLoadBare() {
	sto, err := s.loader.Load(s.endpoint("bare"))
	s.Nil(err)
	s.NotNil(sto)
}

func (s *LoaderSuite) TestMapLoader() {
	ep, err := NewEndpoint("file://test")
	sto := memory.NewStorage()
	s.Nil(err)

	loader := MapLoader{ep.String(): sto}

	ep, err = NewEndpoint("file://test")
	s.Nil(err)

	loaderSto, err := loader.Load(ep)
	s.Nil(err)
	s.Equal(sto, loaderSto)
}
