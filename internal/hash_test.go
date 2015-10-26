package internal

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type HashSuite struct{}

var _ = Suite(&HashSuite{})

func (s *HashSuite) TestComputeHash(c *C) {
	hash := ComputeHash(BlobObject, []byte(""))
	c.Assert(hash.String(), Equals, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")

	hash = ComputeHash(BlobObject, []byte("Hello, World!\n"))
	c.Assert(hash.String(), Equals, "8ab686eafeb1f44702738c8b0f24f2567c36da6d")
}

func (s *HashSuite) TestNewHash(c *C) {
	hash := ComputeHash(BlobObject, []byte("Hello, World!\n"))

	c.Assert(hash, Equals, NewHash(hash.String()))
}
