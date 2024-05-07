package file

import (
	"context"

	"github.com/go-git/go-git/v5/internal/transport/test"
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
	path := fixture.DotGit().Root()
	s.Endpoint = prepareRepo(c, path)

	fixture = fixtures.ByTag("empty").One()
	path = fixture.DotGit().Root()
	s.EmptyEndpoint = prepareRepo(c, path)

	s.NonExistentEndpoint = prepareRepo(c, "/non-existent")
}

func (s *ReceivePackSuite) TearDownTest(c *C) {
	s.Suite.TearDownSuite(c)
}

func (s *ReceivePackSuite) TestCommandNoOutput(c *C) {
	client := NewTransport()
	session, err := client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := session.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
	conn.Close()
}

func (s *ReceivePackSuite) TestMalformedInputNoErrors(c *C) {
	client := NewTransport()
	session, err := client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := session.Handshake(context.TODO(), true)
	c.Assert(err, NotNil)
	c.Assert(conn, IsNil)
}

func (s *ReceivePackSuite) TestNonExistentCommand(c *C) {
	client := NewTransport()
	session, err := client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	c.Assert(err, ErrorMatches, ".*(no such file or directory.*|.*file does not exist)*.")
	c.Assert(session, IsNil)
}
