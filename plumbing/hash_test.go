package plumbing

import (
	"testing"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/stretchr/testify/suite"
)

type HashSuite struct {
	suite.Suite
}

func TestHashSuite(t *testing.T) {
	suite.Run(t, new(HashSuite))
}

func (s *HashSuite) TestComputeHash() {
	hash := ComputeHash(BlobObject, []byte(""))
	s.Equal("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", hash.String())

	hash = ComputeHash(BlobObject, []byte("Hello, World!\n"))
	s.Equal("8ab686eafeb1f44702738c8b0f24f2567c36da6d", hash.String())
}

func (s *HashSuite) TestNewHash() {
	hash := ComputeHash(BlobObject, []byte("Hello, World!\n"))
	s.Equal(NewHash(hash.String()), hash)
}

func (s *HashSuite) TestIsZero() {
	hash := NewHash("foo")
	s.True(hash.IsZero())

	hash = NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")
	s.False(hash.IsZero())
}

func (s *HashSuite) TestNewHasher() {
	content := "hasher test sample"
	hasher, err := newHasher(format.SHA1)
	s.Require().NoError(err)
	hash, err := hasher.Compute(BlobObject, []byte(content))
	s.NoError(err)
	s.Equal("dc42c3cc80028d0ec61f0a6b24cadd1c195c4dfc", hash.String())
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
