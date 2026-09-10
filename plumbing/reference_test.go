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

// TestReferenceNameIsSafeAndIsRoot pins IsSafe and IsRoot against the same
// names, because the point of having both is where they disagree. IsSafe is
// Git's refname_is_safe and accepts any shouting one-level name, "CONFIG" and
// "SHALLOW" included — those fold onto .git/config and .git/shallow on a
// case-insensitive filesystem, so the safe/!root rows are the gap IsRoot
// closes. IsRoot in turn uses is_root_ref_syntax's wider alphabet, so a name
// with a '-' can be a root ref by spelling and still not be safe: neither
// predicate is a gate on its own.
func (s *ReferenceSuite) TestReferenceNameIsSafeAndIsRoot() {
	for _, tc := range []struct {
		name ReferenceName
		safe bool
		root bool
	}{
		// The root refs: the "*_HEAD" suffix rule, then the irregular names
		// Git's is_root_ref lists explicitly.
		{"HEAD", true, true},
		{"ORIG_HEAD", true, true},
		{"FETCH_HEAD", true, true},
		{"MERGE_HEAD", true, true},
		{"CHERRY_PICK_HEAD", true, true},
		{"REVERT_HEAD", true, true},
		{"REBASE_HEAD", true, true},
		{"BISECT_HEAD", true, true},
		{"_HEAD", true, true},
		{"AUTO_MERGE", true, true},
		{"BISECT_EXPECTED_REV", true, true},
		{"NOTES_MERGE_PARTIAL", true, true},
		{"NOTES_MERGE_REF", true, true},
		{"MERGE_AUTOSTASH", true, true},
		// is_root_ref_syntax allows '-'; refname_is_safe's [A-Z_] arm does
		// not. A caller that reads IsRoot's "root ref" as permission would
		// accept a name IsSafe refuses.
		{"SOME-THING_HEAD", false, true},
		// Well-formed refs/ names. IsRoot is about the root of the reference
		// store only, so it is false for every one of them.
		{"refs/heads/main", true, false},
		{"refs/heads/release-1.2", true, false},
		{"refs/tags/v1.0.0", true, false},
		{"refs/remotes/origin/HEAD", true, false},
		{"refs/heads/FETCH_HEAD", true, false},
		{"refs/stash", true, false},
		// The uppercase spellings of .git metadata: safe by refname_is_safe,
		// and not root refs. On a case-insensitive filesystem (APFS, NTFS)
		// each folds onto the real file, which is why IsSafe cannot be the
		// only gate a create or update goes through.
		{"CONFIG", true, false},
		{"INDEX", true, false},
		{"SHALLOW", true, false},
		{"PACKED_REFS", true, false},
		{"DESCRIPTION", true, false},
		{"COMMONDIR", true, false},
		{"GITDIR", true, false},
		{"LOGS", true, false},
		{"OBJECTS", true, false},
		{"REFS", true, false},
		{"HOOKS", true, false},
		{"INFO", true, false},
		{"WORKTREES", true, false},
		{"MODULES", true, false},
		{"BRANCHES", true, false},
		{"REMOTES", true, false},
		{"COMMIT_EDITMSG", true, false},
		{"MERGE_MSG", true, false},
		{"MERGE_RR", true, false},
		{"SEQUENCER", true, false},
		// A '-' is outside refname_is_safe's one-level alphabet, so the
		// uppercase spelling of "packed-refs" is not safe either.
		{"PACKED-REFS", false, false},
		// Empty, and one-level names that would land on top-level .git
		// metadata without even needing a case-insensitive filesystem.
		{"", false, false},
		{"config", false, false},
		{"index", false, false},
		{"packed-refs", false, false},
		{"config.worktree", false, false},
		{"bar", false, false},
		{"head", false, false},
		{"Head", false, false},
		{"orig_head", false, false},
		{"HEAD2", false, false},
		{"HEAD.lock", false, false},
		{"HEAD/x", false, false},
		// refs/ names that escape or have empty components.
		{"refs/", false, false},
		{"refs/heads/.", false, false},
		{"refs/heads/..", false, false},
		{"refs/heads/../../config", false, false},
		{"refs/heads//main", false, false},
		{"refs/heads/", false, false},
		// Absolute and drive-prefixed forms.
		{"/HEAD", false, false},
		{"/config", false, false},
		{"/refs/heads/main", false, false},
		{"\\config", false, false},
		{"C:config", false, false},
		// Backslash inside a refs/ name: a Windows path separator that could
		// escape the sub-tree or alias another name once turned into a path.
		{"refs/heads/foo\\bar", false, false},
		{"refs/heads\\..\\config", false, false},
		{"refs/heads\\foo", false, false},
	} {
		s.Equal(tc.safe, tc.name.IsSafe(), "IsSafe(%q)", tc.name)
		s.Equal(tc.root, tc.name.IsRoot(), "IsRoot(%q)", tc.name)
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

// The four gates that draw a line at refs/ used to each carry their own copy
// of the string. This is the one they share.
func (s *ReferenceSuite) TestIsUnderRefs() {
	for _, tc := range []struct {
		name ReferenceName
		want bool
	}{
		{"refs/heads/main", true},
		{"refs/stash", true},
		{"refs/", true},
		{"HEAD", false},
		{"ORIG_HEAD", false},
		{"CONFIG", false},
		{"", false},
		{"refs", false},
		{"Refs/heads/main", false},
		{"xrefs/heads/main", false},
	} {
		s.Equal(tc.want, tc.name.IsUnderRefs(), "IsUnderRefs(%q)", tc.name)
	}

	// The exported prefixes are what the constructors build from, so a change
	// to one cannot silently disagree with the other.
	s.Equal(RefPrefix+"heads/", RefHeadPrefix)
	s.True(NewBranchReferenceName("x").IsUnderRefs())
	s.Equal(ReferenceName(RefHeadPrefix+"x"), NewBranchReferenceName("x"))
}
