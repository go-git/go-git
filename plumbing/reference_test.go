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

func (s *ReferenceSuite) TestReferenceNameIsSafe() {
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
		s.Equal(tc.safe, tc.name.IsSafe(), "IsSafe(%q)", tc.name)
	}
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

// The two creation helpers carry the rules Git keeps in check_branch_ref and
// check_tag_ref rather than in check_refname_format: a shorthand beginning with
// "-", and the spelling that would land on HEAD. They apply to authoring a
// name, not to handling one that already exists, which is why they sit here
// rather than in Validate.
func (s *ReferenceSuite) TestValidateBranchName() {
	for _, name := range []string{"foo", "foo/bar", "release-1.0", "v1.0"} {
		s.NoError(ValidateBranchName(name), "branch name %q", name)
	}

	for _, name := range []string{"-foo", "-", "HEAD", "", "foo..bar", "foo.lock", "foo bar"} {
		err := ValidateBranchName(name)
		s.ErrorIs(err, ErrInvalidReferenceName, "branch name %q", name)
	}
}

func (s *ReferenceSuite) TestValidateTagName() {
	for _, name := range []string{"v1.0.0", "release/v1", "v1-rc1"} {
		s.NoError(ValidateTagName(name), "tag name %q", name)
	}

	for _, name := range []string{"-1.0", "-", "HEAD", "", "v1..0", "v1.lock", "v 1"} {
		err := ValidateTagName(name)
		s.ErrorIs(err, ErrInvalidReferenceName, "tag name %q", name)
	}
}

// The names the two helpers refuse for their own reasons are names Validate
// accepts, which is what keeps a repository that already holds one usable.
func (s *ReferenceSuite) TestCreationRulesAreNotNamingRules() {
	for _, r := range []ReferenceName{
		"refs/heads/-foo", "refs/heads/HEAD", "refs/tags/-1.0", "refs/tags/HEAD",
	} {
		s.NoError(r.Validate(), "reference name %q", r)
	}

	s.ErrorIs(ValidateBranchName("-foo"), ErrInvalidReferenceName)
	s.ErrorIs(ValidateBranchName("HEAD"), ErrInvalidReferenceName)
	s.ErrorIs(ValidateTagName("-1.0"), ErrInvalidReferenceName)
	s.ErrorIs(ValidateTagName("HEAD"), ErrInvalidReferenceName)
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
		"refs/ab/-testing",
		"refs/123-testing",
		// A leading "-" is not a check_refname_format rule at any position,
		// and git clone, git fetch and git update-ref all handle these. What
		// refuses them is branch and tag creation; see TestValidateBranchName.
		"refs/-",
		"refs/heads/-",
		"refs/heads/-foo",
		"refs/tags/-",
		"refs/tags/-foo",
		// Rule 9 is about a name that is the single character "@", so "@" is
		// an ordinary component. git branch -- @ creates the first of these.
		"refs/heads/@",
		"refs/heads/@/x",
		"refs/heads/@foo",
		"refs/remotes/origin/@",
	}
	for _, v := range valid {
		s.NoError(v.Validate(), "reference name %q", v)
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
		"refs/heads/@{bar}",
		"refs/heads/\n",
		"refs/heads/foo..bar",
		// Rule 9, the whole name and nothing shorter.
		"@",
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
