package ssh

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct{}

var _ = Suite(&ClientSuite{})

func (s *ClientSuite) TestNewClient(c *C) {
	c.Assert(DefaultClient, DeepEquals, NewClient())
}
