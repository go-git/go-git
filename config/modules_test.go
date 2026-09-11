package config

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/pathutil"
)

type ModulesSuite struct {
	suite.Suite
}

func TestModulesSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ModulesSuite))
}

// dotdotDisguises are the spellings a filesystem folds back to a
// parent hop. Both a submodule name and a submodule path must refuse
// every one of them, so both tables consume this slice.
//
// The two tables are deliberately not otherwise symmetric: a name is
// held to the stricter rule because it becomes a directory under
// .git/modules via dotgit.DotGit.Module, while a path is held to
// pathutil.ValidSubmodulePath's rule because Submodule.Repository runs
// that validator on it. A component of periods alone is therefore
// rejected as a name and accepted as a path.
var dotdotDisguises = []string{
	// Literal, at every position and with both separators.
	`..`,
	`../`,
	`../bar`,
	`/..`,
	`/../bar`,
	`foo/..`,
	`foo/../`,
	`foo/../bar`,
	`.\..\foo`,

	// HFS+ parent-disguise policy, independent of native alias resolution.
	".\u200c.",
	"\u200c..",
	"..\u200c",
	"\u200c.\u200d.\u200e",
	"foo/.\u200c.",
	"a/.\u200c./b",

	// NTFS parent-disguise policy, applied on every host.
	".. ",
	"..  ",
	".. .",
	"..:foo",
	"..::$INDEX_ALLOCATION",
	"foo/.. /bar",
	"a/.. /b",
}

// dotDisguises are the spellings a filesystem folds back to the
// directory holding them. A path built from one addresses that
// directory: Submodule.Repository would chroot the submodule worktree
// to the superproject worktree root, and DotGit.Module would chroot
// the module storer to .git/modules itself. Both a name and a path
// must refuse every one of them.
//
// The literal "." belongs here too, but the existing tables already
// carry it, so it is not repeated.
var dotDisguises = []string{
	// HFS+ drops ignorable code points during normalisation.
	".\u200c",
	"\u200c.",
	".\u200e",
	".\ufeff",
	"\u200d.\u200d",
	"foo/.\u200c",
	"a/.\u200c/b",

	// NTFS trims a trailing run of spaces and periods, and reads a
	// colon as an Alternate Data Stream suffix.
	". ",
	".  ",
	". .",
	".:foo",
	".:$DATA",
	".::$INDEX_ALLOCATION",
	"foo/. /bar",
	"a/. /b",
}

func (s *ModulesSuite) TestValidateMissingURL() {
	m := &Submodule{Name: "foo", Path: "foo"}
	s.Equal(ErrModuleEmptyURL, m.Validate())
}

func (s *ModulesSuite) TestValidateBadPath() {
	input := append([]string{".", "./sub", "sub/."}, dotdotDisguises...)
	input = append(input, dotDisguises...)
	for _, p := range input {
		m := &Submodule{
			Name: "ok",
			Path: p,
			URL:  "https://example.com/",
		}
		s.Equal(ErrModuleBadPath, m.Validate(), "path %q", p)
	}
}

func (s *ModulesSuite) TestValidateGoodPath() {
	// The boundary rows: a component of periods alone, and a ".."
	// prefix whose tail does not fold, are legitimate names that
	// C Git accepts on POSIX. pathutil.ValidSubmodulePath accepts
	// them, so Validate accepts them too, and they survive parsing to
	// reach Submodule.Repository.
	//
	// ". " and ". ." are not on this list: they fold to the directory
	// holding them, which would scope the submodule worktree to the
	// superproject root. dotDisguises carries them.
	for _, p := range []string{
		"foo", "foo/bar", "a..b", "deps/x.y", "lib-foo/sub",
		"...", "....", "a/.../b", "x..", "foo..", "..x", ".. x",
	} {
		m := &Submodule{
			Name: "ok",
			Path: p,
			URL:  "https://example.com/",
		}
		s.NoError(m.Validate(), "path %q", p)
	}
}

// unmarshalSubmodules drops a stanza only on ErrModuleBadPath or
// ErrModuleBadName, so an unsafe Path must be reported as
// ErrModuleBadPath even when a required field is also missing.
// Otherwise the stanza is retained with the unsafe Path intact.
func (s *ModulesSuite) TestValidateBadPathBeatsEmptyURL() {
	m := &Submodule{Name: "ok", Path: "..", URL: ""}
	s.Equal(ErrModuleBadPath, m.Validate())
}

func (s *ModulesSuite) TestValidateBadPathBeatsEmptyURLDisguised() {
	m := &Submodule{Name: "ok", Path: ".. ", URL: ""}
	s.Equal(ErrModuleBadPath, m.Validate())
}

func (s *ModulesSuite) TestValidateEmptyPathStillReportsEmptyPath() {
	m := &Submodule{Name: "ok", Path: "", URL: ""}
	s.Equal(ErrModuleEmptyPath, m.Validate())
}

func (s *ModulesSuite) TestUnmarshalDropsBadPathStanzaWithMissingURL() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \"m\"]\n\tpath = ..\n")))
	s.Empty(m.Submodules, "stanza with an unsafe path must be dropped")
}

func (s *ModulesSuite) TestUnmarshalDropsDisguisedBadPathStanzaWithMissingURL() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \"m\"]\n\tpath = .. \n")))
	s.Empty(m.Submodules, "stanza with a disguised unsafe path must be dropped")
}

func (s *ModulesSuite) TestValidateMissingName() {
	m := &Submodule{Name: "ok", URL: "bar"}
	s.Equal(ErrModuleEmptyPath, m.Validate())
}

func (s *ModulesSuite) TestValidateBadName() {
	// Submodule storage names apply an additional periods-only policy.
	input := append([]string{
		"",
		".",
		"....",
		"a/.", "./a", "a/./b",
		"/abs",
		`C:\win`,
		"x\x00y",
		"x/",
		"/x",
		"modules/../escape",
		"a/../../b",
	}, dotdotDisguises...)
	input = append(input, dotDisguises...)
	for _, n := range input {
		m := &Submodule{
			Name: n,
			Path: "ok",
			URL:  "https://example.com/",
		}
		// Validate wraps the sentinel with the offending name
		// (canonical-Git wording: "ignoring suspicious submodule
		// name: <name>"), so use ErrorIs.
		s.ErrorIs(m.Validate(), ErrModuleBadName, "name %q", n)
	}
}

func (s *ModulesSuite) TestValidateGoodName() {
	for _, n := range []string{"foo", "lib-foo", "deps/x", "x.y"} {
		m := &Submodule{
			Name: n,
			Path: "ok",
			URL:  "https://example.com/",
		}
		s.NoError(m.Validate(), "name %q", n)
	}
}

func (s *ModulesSuite) TestMarshal() {
	input := []byte(`[submodule "qux"]
	path = qux
	url = baz
	branch = bar
`)

	cfg := NewModules()
	cfg.Submodules["qux"] = &Submodule{Path: "qux", URL: "baz", Branch: "bar"}

	output, err := cfg.Marshal()
	s.NoError(err)
	s.Equal(input, output)
}

func (s *ModulesSuite) TestUnmarshal() {
	input := []byte(`[submodule "qux"]
        path = qux
        url = https://github.com/foo/qux.git
[submodule "foo/bar"]
        path = foo/bar
        url = https://github.com/foo/bar.git
		branch = dev
[submodule "suspicious"]
        path = ../../foo/bar
        url = https://github.com/foo/bar.git
[submodule ".."]
        path = deps/x
        url = https://github.com/foo/bar.git
`)

	cfg := NewModules()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	// The "suspicious" entry is dropped because of its `..` path,
	// and the `..` entry is dropped because of its suspicious name
	// (canonical Git's "ignoring suspicious submodule name" rule).
	s.Len(cfg.Submodules, 2)
	s.Equal("qux", cfg.Submodules["qux"].Name)
	s.Equal("https://github.com/foo/qux.git", cfg.Submodules["qux"].URL)
	s.Equal("foo/bar", cfg.Submodules["foo/bar"].Name)
	s.Equal("https://github.com/foo/bar.git", cfg.Submodules["foo/bar"].URL)
	s.Equal("dev", cfg.Submodules["foo/bar"].Branch)
	s.NotContains(cfg.Submodules, "..")
}

func (s *ModulesSuite) TestUnmarshalMarshal() {
	input := []byte(`[submodule "foo/bar"]
	path = foo/bar
	url = https://github.com/foo/bar.git
	ignore = all
`)

	cfg := NewModules()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	output, err := cfg.Marshal()
	s.NoError(err)
	s.Equal(string(input), string(output))
}

func (s *ModulesSuite) TestUnmarshalDropsDotDisguisedNameStanza() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \". \"]\n\tpath = ok\n\turl = u\n")))
	s.Empty(m.Submodules, "stanza whose name folds to the modules root must be dropped")
}

func (s *ModulesSuite) TestUnmarshalDropsHFSDotDisguisedNameStanza() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \".\u200c\"]\n\tpath = ok\n\turl = u\n")))
	s.Empty(m.Submodules, "stanza whose name folds to the modules root must be dropped")
}

func (s *ModulesSuite) TestUnmarshalDropsDotDisguisedPathStanza() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \"m\"]\n\tpath = . \n\turl = u\n")))
	s.Empty(m.Submodules, "stanza whose path folds to the worktree root must be dropped")
}

// Validate delegates the path to pathutil.ValidSubmodulePath, the same
// validator Submodule.Repository runs before it chroots to that path.
// Every shape the validator refuses must therefore be ErrModuleBadPath
// here, so that a stanza the parser keeps is one whose Path can reach
// the chroot.
func (s *ModulesSuite) TestValidateBadPathMatchesSubmodulePathPolicy() {
	for _, p := range []string{
		// .git and its aliases, at every position and in both
		// separator forms.
		".git",
		".git/config",
		"a/.git",
		"a/.git/b",
		`a\.git\b`,
		".GIT",
		"git~1",
		"sub/git~1/HEAD",

		// NTFS and HFS+ .git disguises.
		"sub/.git . ",
		".git::$INDEX_ALLOCATION",
		"git~1 ",
		"git~1.",
		".g\u200cit",

		// Control bytes.
		"a\x01b",
		"foo\x7fbar",
	} {
		s.Require().Error(pathutil.ValidSubmodulePath(p), "path %q", p)

		m := &Submodule{
			Name: "ok",
			Path: p,
			URL:  "https://example.com/",
		}
		s.Equal(ErrModuleBadPath, m.Validate(), "path %q", p)
	}
}

func (s *ModulesSuite) TestUnmarshalDropsDotGitPathStanza() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \"m\"]\n\tpath = a/.git/b\n\turl = u\n")))
	s.Empty(m.Submodules, "stanza whose path carries a .git component must be dropped")
}

func (s *ModulesSuite) TestUnmarshalDropsDotGitPathStanzaWithMissingURL() {
	m := NewModules()
	s.Require().NoError(m.Unmarshal([]byte("[submodule \"m\"]\n\tpath = .git\n")))
	s.Empty(m.Submodules, "stanza whose path names .git must be dropped")
}
