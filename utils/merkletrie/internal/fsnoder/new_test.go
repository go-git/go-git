package fsnoder

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type FSNoderSuite struct {
	suite.Suite
}

func TestFSNoderSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(FSNoderSuite))
}

func check(s *FSNoderSuite, input string, expected *dir) {
	obtained, err := New(input)
	s.NoError(err, fmt.Sprintf("input = %s", input))

	comment := fmt.Sprintf("\n   input = %s\n"+
		"expected = %s\nobtained = %s",
		input, expected, obtained)
	s.Equal(expected.Hash(), obtained.Hash(), comment)
}

func (s *FSNoderSuite) TestNoDataFails() {
	_, err := New("")
	s.Error(err)

	_, err = New(" 	") // SPC + TAB
	s.Error(err)
}

func (s *FSNoderSuite) TestUnnamedRootFailsIfNotRoot() {
	_, err := decodeDir([]byte("()"), false)
	s.Error(err)
}

func (s *FSNoderSuite) TestUnnamedInnerFails() {
	_, err := New("(())")
	s.Error(err)
	_, err = New("((a<>))")
	s.Error(err)
}

func (s *FSNoderSuite) TestMalformedFile() {
	_, err := New("(4<>)")
	s.Error(err)
	_, err = New("(4<1>)")
	s.Error(err)
	_, err = New("(4?1>)")
	s.Error(err)
	_, err = New("(4<a>)")
	s.Error(err)
	_, err = New("(4<a?)")
	s.Error(err)

	_, err = decodeFile([]byte("a?1>"))
	s.Error(err)
	_, err = decodeFile([]byte("a<a>"))
	s.Error(err)
	_, err = decodeFile([]byte("a<1?"))
	s.Error(err)

	_, err = decodeFile([]byte("a?>"))
	s.Error(err)
	_, err = decodeFile([]byte("1<>"))
	s.Error(err)
	_, err = decodeFile([]byte("a<?"))
	s.Error(err)
}

func (s *FSNoderSuite) TestMalformedRootFails() {
	_, err := New(")")
	s.Error(err)
	_, err = New("(")
	s.Error(err)
	_, err = New("(a<>")
	s.Error(err)
	_, err = New("a<>")
	s.Error(err)
}

func (s *FSNoderSuite) TestUnnamedEmptyRoot() {
	input := "()"

	expected, err := newDir("", nil)
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestNamedEmptyRoot() {
	input := "a()"

	expected, err := newDir("a", nil)
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestEmptyFile() {
	input := "(a<>)"

	a1, err := newFile("a", "")
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{a1})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestNonEmptyFile() {
	input := "(a<1>)"

	a1, err := newFile("a", "1")
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{a1})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestTwoFilesSameContents() {
	input := "(b<1> a<1>)"

	a1, err := newFile("a", "1")
	s.NoError(err)
	b1, err := newFile("b", "1")
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{a1, b1})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestTwoFilesDifferentContents() {
	input := "(b<2> a<1>)"

	a1, err := newFile("a", "1")
	s.NoError(err)
	b2, err := newFile("b", "2")
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{a1, b2})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestManyFiles() {
	input := "(e<1> b<2> a<1> c<1> d<3> f<4>)"

	a1, err := newFile("a", "1")
	s.NoError(err)
	b2, err := newFile("b", "2")
	s.NoError(err)
	c1, err := newFile("c", "1")
	s.NoError(err)
	d3, err := newFile("d", "3")
	s.NoError(err)
	e1, err := newFile("e", "1")
	s.NoError(err)
	f4, err := newFile("f", "4")
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{e1, b2, a1, c1, d3, f4})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestEmptyDir() {
	input := "(A())"

	A, err := newDir("A", nil)
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithEmptyFile() {
	input := "(A(a<>))"

	a, err := newFile("a", "")
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{a})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithEmptyFileSameName() {
	input := "(A(A<>))"

	f, err := newFile("A", "")
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{f})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithFileLongContents() {
	input := "(A(a<12>))"

	a1, err := newFile("a", "12")
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{a1})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithFileLongName() {
	input := "(A(abc<12>))"

	a1, err := newFile("abc", "12")
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{a1})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithFile() {
	input := "(A(a<1>))"

	a1, err := newFile("a", "1")
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{a1})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithEmptyDirSameName() {
	input := "(A(A()))"

	A2, err := newDir("A", nil)
	s.NoError(err)
	A1, err := newDir("A", []noder.Noder{A2})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A1})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithEmptyDir() {
	input := "(A(B()))"

	B, err := newDir("B", nil)
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{B})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestDirWithTwoFiles() {
	input := "(A(a<1> b<2>))"

	a1, err := newFile("a", "1")
	s.NoError(err)
	b2, err := newFile("b", "2")
	s.NoError(err)
	A, err := newDir("A", []noder.Noder{b2, a1})
	s.NoError(err)
	expected, err := newDir("", []noder.Noder{A})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestCrazy() {
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
	input := "(d<2> b(c<1> b() a() x(a<1>)) a<1> c<1> e(e(e(e<1>))))"

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

	expected, err := newDir("", []noder.Noder{a1, d2, E, B, c1})
	s.NoError(err)

	check(s, input, expected)
}

func (s *FSNoderSuite) TestHashEqual() {
	input1 := "(A(a<1> b<2>))"
	input2 := "(A(a<1> b<2>))"
	input3 := "(A(a<> b<2>))"

	t1, err := New(input1)
	s.NoError(err)
	t2, err := New(input2)
	s.NoError(err)
	t3, err := New(input3)
	s.NoError(err)

	s.True(HashEqual(t1, t2))
	s.True(HashEqual(t2, t1))

	s.False(HashEqual(t2, t3))
	s.False(HashEqual(t3, t2))

	s.False(HashEqual(t3, t1))
	s.False(HashEqual(t1, t3))
}
