package plumbing

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ReferenceSuite struct {
	suite.Suite
}

func TestReferenceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReferenceSuite))
}

const (
	ExampleReferenceName ReferenceName = "refs/heads/v4"
)

func (s *ReferenceSuite) TestReferenceTypeString() {
	s.Equal("symbolic-reference", SymbolicReference.String())
}

func (s *ReferenceSuite) TestReferenceNameShort() {
	s.Equal("v4", ExampleReferenceName.Short())
}

func (s *ReferenceSuite) TestReferenceNameWithSlash() {
	r := ReferenceName("refs/remotes/origin/feature/AllowSlashes")
	s.Equal("origin/feature/AllowSlashes", r.Short())
}

func (s *ReferenceSuite) TestReferenceNameNote() {
	r := ReferenceName("refs/notes/foo")
	s.Equal("notes/foo", r.Short())
}

func (s *ReferenceSuite) TestNewReferenceFromStrings() {
	r := NewReferenceFromStrings("refs/heads/v4", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	s.Equal(HashReference, r.Type())
	s.Equal(ExampleReferenceName, r.Name())
	s.Equal(NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), r.Hash())

	r = NewReferenceFromStrings("HEAD", "ref: refs/heads/v4")
	s.Equal(SymbolicReference, r.Type())
	s.Equal(HEAD, r.Name())
	s.Equal(ExampleReferenceName, r.Target())
}

func (s *ReferenceSuite) TestNewSymbolicReference() {
	r := NewSymbolicReference(HEAD, ExampleReferenceName)
	s.Equal(SymbolicReference, r.Type())
	s.Equal(HEAD, r.Name())
	s.Equal(ExampleReferenceName, r.Target())
}

func (s *ReferenceSuite) TestNewHashReference() {
	r := NewHashReference(ExampleReferenceName, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	s.Equal(HashReference, r.Type())
	s.Equal(ExampleReferenceName, r.Name())
	s.Equal(NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), r.Hash())
}

func (s *ReferenceSuite) TestNewBranchReferenceName() {
	r := NewBranchReferenceName("foo")
	s.Equal("refs/heads/foo", r.String())
}

func (s *ReferenceSuite) TestNewNoteReferenceName() {
	r := NewNoteReferenceName("foo")
	s.Equal("refs/notes/foo", r.String())
}

func (s *ReferenceSuite) TestNewRemoteReferenceName() {
	r := NewRemoteReferenceName("bar", "foo")
	s.Equal("refs/remotes/bar/foo", r.String())
}

func (s *ReferenceSuite) TestNewRemoteHEADReferenceName() {
	r := NewRemoteHEADReferenceName("foo")
	s.Equal("refs/remotes/foo/HEAD", r.String())
}

func (s *ReferenceSuite) TestNewTagReferenceName() {
	r := NewTagReferenceName("foo")
	s.Equal("refs/tags/foo", r.String())
}

func (s *ReferenceSuite) TestIsBranch() {
	r := ExampleReferenceName
	s.True(r.IsBranch())
}

func (s *ReferenceSuite) TestIsNote() {
	r := ReferenceName("refs/notes/foo")
	s.True(r.IsNote())
}

func (s *ReferenceSuite) TestIsRemote() {
	r := ReferenceName("refs/remotes/origin/master")
	s.True(r.IsRemote())
}

func (s *ReferenceSuite) TestIsTag() {
	r := ReferenceName("refs/tags/v3.1.")
	s.True(r.IsTag())
}

func (s *ReferenceSuite) TestValidReferenceNames() {
	valid := []ReferenceName{
		"refs/heads/master",
		"refs/notes/commits",
		"refs/remotes/origin/master",
		"HEAD",
		"refs/tags/v3.1.1",
		"refs/pulls/1/head",
		"refs/pulls/1/merge",
		"refs/pulls/1/abc.123",
		"refs/pulls",
		"refs/-", // should this be allowed?
		"refs/ab/-testing",
		"refs/123-testing",
	}
	for _, v := range valid {
		s.Nil(v.Validate())
	}

	invalid := []ReferenceName{
		"refs",
		"refs/",
		"refs//",
		"refs/heads/\\",
		"refs/heads/\\foo",
		"refs/heads/\\foo/bar",
		"abc",
		"",
		"refs/heads/ ",
		"refs/heads/ /",
		"refs/heads/ /foo",
		"refs/heads/.",
		"refs/heads/..",
		"refs/heads/foo..",
		"refs/heads/foo.lock",
		"refs/heads/foo@{bar}",
		"refs/heads/foo[",
		"refs/heads/foo~",
		"refs/heads/foo^",
		"refs/heads/foo:",
		"refs/heads/foo?",
		"refs/heads/foo*",
		"refs/heads/foo[bar",
		"refs/heads/foo\t",
		"refs/heads/@",
		"refs/heads/@{bar}",
		"refs/heads/\n",
		"refs/heads/-foo",
		"refs/heads/foo..bar",
		"refs/heads/-",
		"refs/tags/-",
		"refs/tags/-foo",
	}

	for i, v := range invalid {
		comment := fmt.Sprintf("invalid reference name case %d: %s", i, v)
		err := v.Validate()
		s.Error(err, comment)
		s.ErrorIs(err, ErrInvalidReferenceName, comment)
		s.ErrorContains(err, "invalid reference name", comment)
		// The reference name is included in the error using %q formatting,
		// so we check for the quoted form to handle control characters.
		quoted := fmt.Sprintf("%q", string(v))
		s.ErrorContains(err, quoted, comment)
	}
}

func benchMarkReferenceString(r *Reference, b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = r.String()
	}
}

func BenchmarkReferenceStringSymbolic(b *testing.B) {
	benchMarkReferenceString(NewSymbolicReference("v3.1.1", "refs/tags/v3.1.1"), b)
}

func BenchmarkReferenceObjectID(b *testing.B) {
	benchMarkReferenceString(NewHashReference("v3.1.1", NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")), b)
}

func BenchmarkReferenceStringInvalid(b *testing.B) {
	benchMarkReferenceString(&Reference{}, b)
}
