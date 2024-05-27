package file

import (
	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	CommonSuite
	test.ReceivePackSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpSuite(c *C) {
	s.CommonSuite.SetUpSuite(c)
	s.ReceivePackSuite.Client = DefaultTransport
}

func (s *ReceivePackSuite) SetUpTest(c *C) {
	fixture := fixtures.Basic().One()
	dot := fixture.DotGit()
	s.Endpoint = prepareRepo(c, dot.Root())
	s.Storer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	fixture = fixtures.ByTag("empty").One()
	dot = fixture.DotGit()
	s.EmptyEndpoint = prepareRepo(c, dot.Root())
	s.EmptyStorer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	s.NonExistentEndpoint = prepareRepo(c, "/non-existent")
	s.NonExistentStorer = memory.NewStorage()
}

func (s *ReceivePackSuite) TearDownTest(c *C) {
	s.Suite.TearDownSuite(c)
}
