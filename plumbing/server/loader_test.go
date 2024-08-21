package server

import (
	"os/exec"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

type loaderSuiteRepo struct {
	bare bool

	path string
}

type LoaderSuite struct {
	Repos map[string]loaderSuiteRepo
}

var _ = Suite(&LoaderSuite{
	Repos: map[string]loaderSuiteRepo{
		"repo": {path: "repo.git"},
		"bare": {path: "bare.git", bare: true},
	},
})

func (s *LoaderSuite) SetUpSuite(c *C) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		c.Skip("git command not found")
	}

	dir := c.MkDir()

	for key, repo := range s.Repos {
		repo.path = filepath.Join(dir, repo.path)
		if repo.bare {
			c.Assert(exec.Command("git", "init", "--bare", repo.path).Run(), IsNil)
		} else {
			c.Assert(exec.Command("git", "init", repo.path).Run(), IsNil)
		}
		s.Repos[key] = repo
	}

}

func (s *LoaderSuite) endpoint(c *C, url string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	return ep
}

func (s *LoaderSuite) TestLoadNonExistent(c *C) {
	sto, err := DefaultLoader.Load(s.endpoint(c, "does-not-exist"))
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(sto, IsNil)
}

func (s *LoaderSuite) TestLoadNonExistentIgnoreHost(c *C) {
	sto, err := DefaultLoader.Load(s.endpoint(c, "https://github.com/does-not-exist"))
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(sto, IsNil)
}

func (s *LoaderSuite) TestLoad(c *C) {
	sto, err := DefaultLoader.Load(s.endpoint(c, s.Repos["repo"].path))
	c.Assert(err, IsNil)
	c.Assert(sto, NotNil)
}

func (s *LoaderSuite) TestLoadBare(c *C) {
	sto, err := DefaultLoader.Load(s.endpoint(c, s.Repos["bare"].path))
	c.Assert(err, IsNil)
	c.Assert(sto, NotNil)
}

func (s *LoaderSuite) TestMapLoader(c *C) {
	ep, err := transport.NewEndpoint("file://test")
	sto := memory.NewStorage()
	c.Assert(err, IsNil)

	loader := MapLoader{ep.String(): sto}

	ep, err = transport.NewEndpoint("file://test")
	c.Assert(err, IsNil)

	loaderSto, err := loader.Load(ep)
	c.Assert(err, IsNil)
	c.Assert(sto, Equals, loaderSto)
}
