package noder

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type NoderSuite struct {
	suite.Suite
}

func TestNoderSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(NoderSuite))
}

type noderMock struct {
	name     string
	hash     []byte
	isDir    bool
	children []Noder
}

func (n noderMock) String() string             { return n.Name() }
func (n noderMock) Hash() []byte               { return n.hash }
func (n noderMock) Name() string               { return n.name }
func (n noderMock) IsDir() bool                { return n.isDir }
func (n noderMock) Children() ([]Noder, error) { return n.children, nil }
func (n noderMock) NumChildren() (int, error)  { return len(n.children), nil }
func (n noderMock) Skip() bool                 { return false }

// Returns a sequence with the noders 3, 2, and 1 from the
// following diagram:
//
//	 3
//	 |
//	 2
//	 |
//	 1
//	/ \
//
// c1  c2
//
// This is also the path of "1".
func nodersFixture() []Noder {
	n1 := &noderMock{
		name:     "1",
		hash:     []byte{0x00, 0x01, 0x02},
		isDir:    true,
		children: childrenFixture(),
	}
	n2 := &noderMock{name: "2"}
	n3 := &noderMock{name: "3"}
	return []Noder{n3, n2, n1}
}

// Returns a collection of 2 noders: c1, c2.
func childrenFixture() []Noder {
	c1 := &noderMock{name: "c1"}
	c2 := &noderMock{name: "c2"}
	return []Noder{c1, c2}
}

// returns nodersFixture as the path of "1".
func pathFixture() Path {
	return Path(nodersFixture())
}

func (s *NoderSuite) TestString() {
	s.Equal("3/2/1", pathFixture().String())
}

func (s *NoderSuite) TestLast() {
	s.Equal("1", pathFixture().Last().Name())
}

func (s *NoderSuite) TestPathImplementsNoder() {
	p := pathFixture()
	s.Equal("1", p.Name())
	s.Equal([]byte{0x00, 0x01, 0x02}, p.Hash())
	s.True(p.IsDir())

	children, err := p.Children()
	s.NoError(err)
	s.Equal(childrenFixture(), children)

	numChildren, err := p.NumChildren()
	s.NoError(err)
	s.Equal(2, numChildren)
}
