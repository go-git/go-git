package plumbing

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ObjectSuite struct {
	suite.Suite
}

func TestObjectSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ObjectSuite))
}

func (s *ObjectSuite) TestObjectTypeString() {
	s.Equal("commit", CommitObject.String())
	s.Equal("tree", TreeObject.String())
	s.Equal("blob", BlobObject.String())
	s.Equal("tag", TagObject.String())
	s.Equal("ref-delta", REFDeltaObject.String())
	s.Equal("ofs-delta", OFSDeltaObject.String())
	s.Equal("any", AnyObject.String())
	s.Equal("unknown", ObjectType(42).String())
}

func (s *ObjectSuite) TestObjectTypeBytes() {
	s.Equal([]byte("commit"), CommitObject.Bytes())
}

func (s *ObjectSuite) TestObjectTypeValid() {
	s.True(CommitObject.Valid())
	s.False(ObjectType(42).Valid())
}

func (s *ObjectSuite) TestParseObjectType() {
	for st, e := range map[string]ObjectType{
		"commit":    CommitObject,
		"tree":      TreeObject,
		"blob":      BlobObject,
		"tag":       TagObject,
		"ref-delta": REFDeltaObject,
		"ofs-delta": OFSDeltaObject,
	} {
		t, err := ParseObjectType(st)
		s.NoError(err)
		s.Equal(t, e)
	}

	t, err := ParseObjectType("foo")
	s.ErrorIs(err, ErrInvalidType)
	s.Equal(InvalidObject, t)
}
