package packfile

import (
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type ObjectToPackSuite struct {
	suite.Suite
}

func TestObjectToPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ObjectToPackSuite))
}

func (s *ObjectToPackSuite) TestObjectToPack() {
	obj := &dummyObject{}
	otp := newObjectToPack(obj)
	s.Equal(otp.Object, obj)
	s.Equal(otp.Original, obj)
	s.Nil(otp.Base)
	s.False(otp.IsDelta())

	original := &dummyObject{}
	delta := &dummyObject{}
	deltaToPack := newDeltaObjectToPack(otp, original, delta)
	s.Equal(deltaToPack.Object, obj)
	s.Equal(deltaToPack.Original, original)
	s.Equal(deltaToPack.Base, otp)
	s.True(deltaToPack.IsDelta())
}

type dummyObject struct{}

func (*dummyObject) Hash() plumbing.Hash             { return plumbing.ZeroHash }
func (*dummyObject) Type() plumbing.ObjectType       { return plumbing.InvalidObject }
func (*dummyObject) SetType(plumbing.ObjectType)     {}
func (*dummyObject) Size() int64                     { return 0 }
func (*dummyObject) SetSize(_ int64)                 {}
func (*dummyObject) Reader() (io.ReadCloser, error)  { return nil, nil }
func (*dummyObject) Writer() (io.WriteCloser, error) { return nil, nil }
