package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
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
// ValidTreePath's rule because Submodule.Repository runs
// ValidTreePath on it. A component of periods alone is therefore
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

func (s *ModulesSuite) TestValidateMissingURL() {
	m := &Submodule{Name: "foo", Path: "foo"}
	s.Equal(ErrModuleEmptyURL, m.Validate())
}

func (s *ModulesSuite) TestValidateBadPath() {
	input := append([]string{".", "./sub", "sub/."}, dotdotDisguises...)
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
	// C Git accepts on POSIX. ValidTreePath accepts them too, and
	// this loop must agree with it because Submodule.Repository runs
	// ValidTreePath on the same string.
	for _, p := range []string{
		"foo", "foo/bar", "a..b", "deps/x.y", "lib-foo/sub",
		"...", "....", "a/.../b", "x..", "foo..", "..x", ".. x",
		". ", ". .",
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
