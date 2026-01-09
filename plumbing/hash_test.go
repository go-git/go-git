package plumbing

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type HashSuite struct {
	suite.Suite
}

func TestHashSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(HashSuite))
}

func (s *HashSuite) TestIsZero() {
	hash := NewHash("foo")
	s.True(hash.IsZero())

	hash = NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")
	s.False(hash.IsZero())
}

func (s *HashSuite) TestHashesSort() {
	i := []Hash{
		NewHash("2222222222222222222222222222222222222222"),
		NewHash("1111111111111111111111111111111111111111"),
	}

	HashesSort(i)

	s.Equal(NewHash("1111111111111111111111111111111111111111"), i[0])
	s.Equal(NewHash("2222222222222222222222222222222222222222"), i[1])
}

func (s *HashSuite) TestIsHash() {
	s.True(IsHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"))
	s.False(IsHash("foo"))
	s.False(IsHash("zab686eafeb1f44702738c8b0f24f2567c36da6d"))
}
