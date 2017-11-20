package git

import (
	"runtime"

	"github.com/src-d/go-git-fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	test.ReceivePackSuite
	BaseSuite
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpTest(c *C) {
	if runtime.GOOS == "windows" {
		c.Skip(`git for windows has issues with write operations through git:// protocol.
		See https://github.com/git-for-windows/git/issues/907`)
	}

	s.BaseSuite.SetUpTest(c)

	s.ReceivePackSuite.Client = DefaultClient
	s.ReceivePackSuite.Endpoint = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.ReceivePackSuite.EmptyEndpoint = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.ReceivePackSuite.NonExistentEndpoint = s.newEndpoint(c, "non-existent.git")

	s.StartDaemon(c)
}
