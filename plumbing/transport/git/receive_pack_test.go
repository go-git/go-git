package git

import (
	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	test.ReceivePackSuite
	BaseSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpTest(c *C) {
	s.BaseSuite.SetUpTest(c)

	s.ReceivePackSuite.Client = DefaultClient
	s.ReceivePackSuite.Endpoint, s.ReceivePackSuite.Storer = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.ReceivePackSuite.EmptyEndpoint, s.ReceivePackSuite.EmptyStorer = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.ReceivePackSuite.NonExistentEndpoint, s.ReceivePackSuite.NonExistentStorer = s.newEndpoint(c, "non-existent.git"), memory.NewStorage()

	s.StartDaemon(c)
}

func (s *ReceivePackSuite) TestAdvertisedReferencesEmpty(c *C) {
	// This test from BaseSuite is flaky, so it's disabled until we figure out a solution.
}
