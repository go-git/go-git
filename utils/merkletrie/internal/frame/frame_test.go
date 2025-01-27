package frame

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/utils/merkletrie/internal/fsnoder"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
	"github.com/stretchr/testify/suite"
)

type FrameSuite struct {
	suite.Suite
}

func TestFrameSuite(t *testing.T) {
	suite.Run(t, new(FrameSuite))
}

func (s *FrameSuite) TestNewFrameFromEmptyDir() {
	A, err := fsnoder.New("A()")
	s.NoError(err)

	frame, err := New(A)
	s.NoError(err)

	expectedString := `[]`
	s.Equal(expectedString, frame.String())

	first, ok := frame.First()
	s.Nil(first)
	s.False(ok)

	first, ok = frame.First()
	s.Nil(first)
	s.False(ok)

	l := frame.Len()
	s.Equal(0, l)
}

func (s *FrameSuite) TestNewFrameFromNonEmpty() {
	//        _______A/________
	//        |     /  \       |
	//        x    y    B/     C/
	//                         |
	//                         z
	root, err := fsnoder.New("A(x<> y<> B() C(z<>))")
	s.NoError(err)
	frame, err := New(root)
	s.NoError(err)

	expectedString := `["B", "C", "x", "y"]`
	s.Equal(expectedString, frame.String())

	l := frame.Len()
	s.Equal(4, l)

	checkFirstAndDrop(s, frame, "B", true)
	l = frame.Len()
	s.Equal(3, l)

	checkFirstAndDrop(s, frame, "C", true)
	l = frame.Len()
	s.Equal(2, l)

	checkFirstAndDrop(s, frame, "x", true)
	l = frame.Len()
	s.Equal(1, l)

	checkFirstAndDrop(s, frame, "y", true)
	l = frame.Len()
	s.Equal(0, l)

	checkFirstAndDrop(s, frame, "", false)
	l = frame.Len()
	s.Equal(0, l)

	checkFirstAndDrop(s, frame, "", false)
}

func checkFirstAndDrop(s *FrameSuite, f *Frame, expectedNodeName string, expectedOK bool) {
	first, ok := f.First()
	s.Equal(expectedOK, ok)
	if expectedOK {
		s.Equal(expectedNodeName, first.Name())
	}

	f.Drop()
}

// a mock noder that returns error when Children() is called
type errorNoder struct{ noder.Noder }

func (e *errorNoder) Children() ([]noder.Noder, error) {
	return nil, fmt.Errorf("mock error")
}

func (s *FrameSuite) TestNewFrameErrors() {
	_, err := New(&errorNoder{})
	s.ErrorContains(err, "mock error")
}
