package fsnoder

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type FileSuite struct {
	suite.Suite
}

func TestFileSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(FileSuite))
}

var (
	HashOfEmptyFile = []byte{0xcb, 0xf2, 0x9c, 0xe4, 0x84, 0x22, 0x23, 0x25} // fnv64 basis offset
	HashOfContents  = []byte{0xee, 0x7e, 0xf3, 0xd0, 0xc2, 0xb5, 0xef, 0x83} // hash of "contents"
)

func (s *FileSuite) TestNewFileEmpty() {
	f, err := newFile("name", "")
	s.NoError(err)

	s.Equal(HashOfEmptyFile, f.Hash())
	s.Equal("name", f.Name())
	s.False(f.IsDir())
	assertChildren(s.T(), f, noder.NoChildren)
	s.Equal("name<>", f.String())
}

func (s *FileSuite) TestNewFileWithContents() {
	f, err := newFile("name", "contents")
	s.NoError(err)

	s.Equal(HashOfContents, f.Hash())
	s.Equal("name", f.Name())
	s.False(f.IsDir())
	assertChildren(s.T(), f, noder.NoChildren)
	s.Equal("name<contents>", f.String())
}

func (s *FileSuite) TestNewfileErrorEmptyName() {
	_, err := newFile("", "contents")
	s.Error(err)
}

func (s *FileSuite) TestDifferentContentsHaveDifferentHash() {
	f1, err := newFile("name", "contents")
	s.NoError(err)

	f2, err := newFile("name", "foo")
	s.NoError(err)

	s.NotEqual(f2.Hash(), f1.Hash())
}

func (s *FileSuite) TestSameContentsHaveSameHash() {
	f1, err := newFile("name1", "contents")
	s.NoError(err)

	f2, err := newFile("name2", "contents")
	s.NoError(err)

	s.Equal(f2.Hash(), f1.Hash())
}
