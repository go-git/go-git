package plumbing

import (
	"testing"

	. "gopkg.in/check.v1"
)

type ReferenceSuite struct{}

var _ = Suite(&ReferenceSuite{})

const (
	ExampleReferenceName ReferenceName = "refs/heads/v4"
)

func (s *ReferenceSuite) TestReferenceTypeString(c *C) {
	c.Assert(SymbolicReference.String(), Equals, "symbolic-reference")
}

func (s *ReferenceSuite) TestReferenceNameIsSafe(c *C) {
	for _, tc := range []struct {
		name ReferenceName
		safe bool
	}{
		// One-level pseudo-refs ([A-Z_] only).
		{"HEAD", true},
		{"ORIG_HEAD", true},
		{"FETCH_HEAD", true},
		{"MERGE_HEAD", true},
		{"CHERRY_PICK_HEAD", true},
		// Well-formed refs/ names.
		{"refs/heads/main", true},
		{"refs/heads/release-1.2", true},
		{"refs/tags/v1.0.0", true},
		{"refs/remotes/origin/HEAD", true},
		{"refs/stash", true},
		// Empty, and one-level names that are not pseudo-refs (would land on
		// top-level .git metadata).
		{"", false},
		{"config", false},
		{"index", false},
		{"packed-refs", false},
		{"config.worktree", false},
		{"bar", false},
		{"head", false},
		{"HEAD2", false},
		// refs/ names that escape or have empty components.
		{"refs/", false},
		{"refs/heads/.", false},
		{"refs/heads/..", false},
		{"refs/heads/../../config", false},
		{"refs/heads//main", false},
		{"refs/heads/", false},
		// Absolute and drive-prefixed forms.
		{"/config", false},
		{"/refs/heads/main", false},
		{"\\config", false},
		{"C:config", false},
		// Backslash inside a refs/ name: a Windows path separator that could
		// escape the sub-tree or alias another name once turned into a path.
		{"refs/heads/foo\\bar", false},
		{"refs/heads\\..\\config", false},
		{"refs/heads\\foo", false},
	} {
		c.Assert(tc.name.IsSafe(), Equals, tc.safe, Commentf("IsSafe(%q)", tc.name))
	}
}

func (s *ReferenceSuite) TestReferenceNameShort(c *C) {
	c.Assert(ExampleReferenceName.Short(), Equals, "v4")
}

func (s *ReferenceSuite) TestReferenceNameWithSlash(c *C) {
	r := ReferenceName("refs/remotes/origin/feature/AllowSlashes")
	c.Assert(r.Short(), Equals, "origin/feature/AllowSlashes")
}

func (s *ReferenceSuite) TestReferenceNameNote(c *C) {
	r := ReferenceName("refs/notes/foo")
	c.Assert(r.Short(), Equals, "notes/foo")
}

func (s *ReferenceSuite) TestNewReferenceFromStrings(c *C) {
	r := NewReferenceFromStrings("refs/heads/v4", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	c.Assert(r.Type(), Equals, HashReference)
	c.Assert(r.Name(), Equals, ExampleReferenceName)
	c.Assert(r.Hash(), Equals, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	r = NewReferenceFromStrings("HEAD", "ref: refs/heads/v4")
	c.Assert(r.Type(), Equals, SymbolicReference)
	c.Assert(r.Name(), Equals, HEAD)
	c.Assert(r.Target(), Equals, ExampleReferenceName)
}

func (s *ReferenceSuite) TestNewSymbolicReference(c *C) {
	r := NewSymbolicReference(HEAD, ExampleReferenceName)
	c.Assert(r.Type(), Equals, SymbolicReference)
	c.Assert(r.Name(), Equals, HEAD)
	c.Assert(r.Target(), Equals, ExampleReferenceName)
}

func (s *ReferenceSuite) TestNewHashReference(c *C) {
	r := NewHashReference(ExampleReferenceName, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	c.Assert(r.Type(), Equals, HashReference)
	c.Assert(r.Name(), Equals, ExampleReferenceName)
	c.Assert(r.Hash(), Equals, NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}

func (s *ReferenceSuite) TestNewBranchReferenceName(c *C) {
	r := NewBranchReferenceName("foo")
	c.Assert(r.String(), Equals, "refs/heads/foo")
}

func (s *ReferenceSuite) TestNewNoteReferenceName(c *C) {
	r := NewNoteReferenceName("foo")
	c.Assert(r.String(), Equals, "refs/notes/foo")
}

func (s *ReferenceSuite) TestNewRemoteReferenceName(c *C) {
	r := NewRemoteReferenceName("bar", "foo")
	c.Assert(r.String(), Equals, "refs/remotes/bar/foo")
}

func (s *ReferenceSuite) TestNewRemoteHEADReferenceName(c *C) {
	r := NewRemoteHEADReferenceName("foo")
	c.Assert(r.String(), Equals, "refs/remotes/foo/HEAD")
}

func (s *ReferenceSuite) TestNewTagReferenceName(c *C) {
	r := NewTagReferenceName("foo")
	c.Assert(r.String(), Equals, "refs/tags/foo")
}

func (s *ReferenceSuite) TestIsBranch(c *C) {
	r := ExampleReferenceName
	c.Assert(r.IsBranch(), Equals, true)
}

func (s *ReferenceSuite) TestIsNote(c *C) {
	r := ReferenceName("refs/notes/foo")
	c.Assert(r.IsNote(), Equals, true)
}

func (s *ReferenceSuite) TestIsRemote(c *C) {
	r := ReferenceName("refs/remotes/origin/master")
	c.Assert(r.IsRemote(), Equals, true)
}

func (s *ReferenceSuite) TestIsTag(c *C) {
	r := ReferenceName("refs/tags/v3.1.")
	c.Assert(r.IsTag(), Equals, true)
}

func (s *ReferenceSuite) TestValidReferenceNames(c *C) {
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
		c.Assert(v.Validate(), IsNil)
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
		comment := Commentf("invalid reference name case %d: %s", i, v)
		c.Assert(v.Validate(), NotNil, comment)
		c.Assert(v.Validate(), ErrorMatches, "invalid reference name", comment)
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

func BenchmarkReferenceStringHash(b *testing.B) {
	benchMarkReferenceString(NewHashReference("v3.1.1", NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")), b)
}

func BenchmarkReferenceStringInvalid(b *testing.B) {
	benchMarkReferenceString(&Reference{}, b)
}
