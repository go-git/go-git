package fsnoder

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type DirSuite struct {
	suite.Suite
}

func TestDirSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(DirSuite))
}

func (s *DirSuite) TestIsDir() {
	noName, err := newDir("", nil)
	s.NoError(err)
	s.True(noName.IsDir())

	empty, err := newDir("empty", nil)
	s.NoError(err)
	s.True(empty.IsDir())

	root, err := newDir("foo", []noder.Noder{empty})
	s.NoError(err)
	s.True(root.IsDir())
}

func assertChildren(t *testing.T, n noder.Noder, expected []noder.Noder) {
	numChildren, err := n.NumChildren()
	assert.NoError(t, err)
	assert.Len(t, expected, numChildren)

	children, err := n.Children()
	assert.NoError(t, err)
	sort.Sort(byName(children))
	sort.Sort(byName(expected))
	assert.Equal(t, expected, children)
}

func (s *DirSuite) TestNewDirectoryNoNameAndEmpty() {
	root, err := newDir("", nil)
	s.NoError(err)

	s.Equal([]byte{0xca, 0x40, 0xf8, 0x67, 0x57, 0x8c, 0x32, 0x1c}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, noder.NoChildren)
	s.Equal("()", root.String())
}

func (s *DirSuite) TestNewDirectoryEmpty() {
	root, err := newDir("root", nil)
	s.NoError(err)

	s.Equal([]byte{0xca, 0x40, 0xf8, 0x67, 0x57, 0x8c, 0x32, 0x1c}, root.Hash())
	s.Equal("root", root.Name())
	assertChildren(s.T(), root, noder.NoChildren)
	s.Equal("root()", root.String())
}

func (s *DirSuite) TestEmptyDirsHaveSameHash() {
	d1, err := newDir("foo", nil)
	s.NoError(err)

	d2, err := newDir("bar", nil)
	s.NoError(err)

	s.Equal(d2.Hash(), d1.Hash())
}

func (s *DirSuite) TestNewDirWithEmptyDir() {
	empty, err := newDir("empty", nil)
	s.NoError(err)

	root, err := newDir("", []noder.Noder{empty})
	s.NoError(err)

	s.Equal([]byte{0x39, 0x25, 0xa8, 0x99, 0x16, 0x47, 0x6a, 0x75}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, []noder.Noder{empty})
	s.Equal("(empty())", root.String())
}

func (s *DirSuite) TestNewDirWithOneEmptyFile() {
	empty, err := newFile("name", "")
	s.NoError(err)

	root, err := newDir("", []noder.Noder{empty})
	s.NoError(err)
	s.Equal([]byte{0xd, 0x4e, 0x23, 0x1d, 0xf5, 0x2e, 0xfa, 0xc2}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, []noder.Noder{empty})
	s.Equal("(name<>)", root.String())
}

func (s *DirSuite) TestNewDirWithOneFile() {
	a, err := newFile("a", "1")
	s.NoError(err)

	root, err := newDir("", []noder.Noder{a})
	s.NoError(err)
	s.Equal([]byte{0x96, 0xab, 0x29, 0x54, 0x2, 0x9e, 0x89, 0x28}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, []noder.Noder{a})
	s.Equal("(a<1>)", root.String())
}

func (s *DirSuite) TestDirsWithSameFileHaveSameHash() {
	f1, err := newFile("a", "1")
	s.NoError(err)
	r1, err := newDir("", []noder.Noder{f1})
	s.NoError(err)

	f2, err := newFile("a", "1")
	s.NoError(err)
	r2, err := newDir("", []noder.Noder{f2})
	s.NoError(err)

	s.Equal(r2.Hash(), r1.Hash())
}

func (s *DirSuite) TestDirsWithDifferentFileContentHaveDifferentHash() {
	f1, err := newFile("a", "1")
	s.NoError(err)
	r1, err := newDir("", []noder.Noder{f1})
	s.NoError(err)

	f2, err := newFile("a", "2")
	s.NoError(err)
	r2, err := newDir("", []noder.Noder{f2})
	s.NoError(err)

	s.NotEqual(r2.Hash(), r1.Hash())
}

func (s *DirSuite) TestDirsWithDifferentFileNameHaveDifferentHash() {
	f1, err := newFile("a", "1")
	s.NoError(err)
	r1, err := newDir("", []noder.Noder{f1})
	s.NoError(err)

	f2, err := newFile("b", "1")
	s.NoError(err)
	r2, err := newDir("", []noder.Noder{f2})
	s.NoError(err)

	s.NotEqual(r2.Hash(), r1.Hash())
}

func (s *DirSuite) TestDirsWithDifferentFileHaveDifferentHash() {
	f1, err := newFile("a", "1")
	s.NoError(err)
	r1, err := newDir("", []noder.Noder{f1})
	s.NoError(err)

	f2, err := newFile("b", "2")
	s.NoError(err)
	r2, err := newDir("", []noder.Noder{f2})
	s.NoError(err)

	s.NotEqual(r2.Hash(), r1.Hash())
}

func (s *DirSuite) TestDirWithEmptyDirHasDifferentHashThanEmptyDir() {
	f, err := newFile("a", "")
	s.NoError(err)
	r1, err := newDir("", []noder.Noder{f})
	s.NoError(err)

	d, err := newDir("a", nil)
	s.NoError(err)
	r2, err := newDir("", []noder.Noder{d})
	s.NoError(err)

	s.NotEqual(r2.Hash(), r1.Hash())
}

func (s *DirSuite) TestNewDirWithTwoFilesSameContent() {
	a1, err := newFile("a", "1")
	s.NoError(err)
	b1, err := newFile("b", "1")
	s.NoError(err)

	root, err := newDir("", []noder.Noder{a1, b1})
	s.NoError(err)

	s.Equal([]byte{0xc7, 0xc4, 0xbf, 0x70, 0x33, 0xb9, 0x57, 0xdb}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, []noder.Noder{b1, a1})
	s.Equal("(a<1> b<1>)", root.String())
}

func (s *DirSuite) TestNewDirWithTwoFilesDifferentContent() {
	a1, err := newFile("a", "1")
	s.NoError(err)
	b2, err := newFile("b", "2")
	s.NoError(err)

	root, err := newDir("", []noder.Noder{a1, b2})
	s.NoError(err)

	s.Equal([]byte{0x94, 0x8a, 0x9d, 0x8f, 0x6d, 0x98, 0x34, 0x55}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, []noder.Noder{b2, a1})
}

func (s *DirSuite) TestCrazy() {
	//           ""
	//            |
	//   -------------------------
	//   |    |      |      |    |
	//  a1    B     c1     d2    E
	//        |                  |
	//   -------------           E
	//   |   |   |   |           |
	//   A   B   X   c1          E
	//           |               |
	//          a1               e1
	e1, err := newFile("e", "1")
	s.NoError(err)
	E, err := newDir("e", []noder.Noder{e1})
	s.NoError(err)
	E, err = newDir("e", []noder.Noder{E})
	s.NoError(err)
	E, err = newDir("e", []noder.Noder{E})
	s.NoError(err)

	A, err := newDir("a", nil)
	s.NoError(err)
	B, err := newDir("b", nil)
	s.NoError(err)
	a1, err := newFile("a", "1")
	s.NoError(err)
	X, err := newDir("x", []noder.Noder{a1})
	s.NoError(err)
	c1, err := newFile("c", "1")
	s.NoError(err)
	B, err = newDir("b", []noder.Noder{c1, B, X, A})
	s.NoError(err)

	a1, err = newFile("a", "1")
	s.NoError(err)
	c1, err = newFile("c", "1")
	s.NoError(err)
	d2, err := newFile("d", "2")
	s.NoError(err)

	root, err := newDir("", []noder.Noder{a1, d2, E, B, c1})
	s.NoError(err)

	s.Equal([]byte{0xc3, 0x72, 0x9d, 0xf1, 0xcc, 0xec, 0x6d, 0xbb}, root.Hash())
	s.Equal("", root.Name())
	assertChildren(s.T(), root, []noder.Noder{E, c1, B, a1, d2})
	s.Equal("(a<1> b(a() b() c<1> x(a<1>)) c<1> d<2> e(e(e(e<1>))))", root.String())
}

func (s *DirSuite) TestDirCannotHaveDirWithNoName() {
	noName, err := newDir("", nil)
	s.NoError(err)

	_, err = newDir("", []noder.Noder{noName})
	s.Error(err)
}

func (s *DirSuite) TestDirCannotHaveDuplicatedFiles() {
	f1, err := newFile("a", "1")
	s.NoError(err)

	f2, err := newFile("a", "1")
	s.NoError(err)

	_, err = newDir("", []noder.Noder{f1, f2})
	s.Error(err)
}

func (s *DirSuite) TestDirCannotHaveDuplicatedFileNames() {
	a1, err := newFile("a", "1")
	s.NoError(err)

	a2, err := newFile("a", "2")
	s.NoError(err)

	_, err = newDir("", []noder.Noder{a1, a2})
	s.Error(err)
}

func (s *DirSuite) TestDirCannotHaveDuplicatedDirNames() {
	d1, err := newDir("a", nil)
	s.NoError(err)

	d2, err := newDir("a", nil)
	s.NoError(err)

	_, err = newDir("", []noder.Noder{d1, d2})
	s.Error(err)
}

func (s *DirSuite) TestDirCannotHaveDirAndFileWithSameName() {
	f, err := newFile("a", "")
	s.NoError(err)

	d, err := newDir("a", nil)
	s.NoError(err)

	_, err = newDir("", []noder.Noder{f, d})
	s.Error(err)
}

func (s *DirSuite) TestUnsortedString() {
	b, err := newDir("b", nil)
	s.NoError(err)

	z, err := newDir("z", nil)
	s.NoError(err)

	a1, err := newFile("a", "1")
	s.NoError(err)

	c2, err := newFile("c", "2")
	s.NoError(err)

	d3, err := newFile("d", "3")
	s.NoError(err)

	d, err := newDir("d", []noder.Noder{c2, z, d3, a1, b})
	s.NoError(err)

	s.Equal("d(a<1> b() c<2> d<3> z())", d.String())
}
