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

func (s *ModulesSuite) TestValidateMissingURL() {
	m := &Submodule{Name: "foo", Path: "foo"}
	s.Equal(ErrModuleEmptyURL, m.Validate())
}

func (s *ModulesSuite) TestValidateBadPath() {
	input := []string{
		`..`,
		`../`,
		`../bar`,

		`/..`,
		`/../bar`,

		`foo/..`,
		`foo/../`,
		`foo/../bar`,
	}

	for _, p := range input {
		m := &Submodule{
			Name: "ok",
			Path: p,
			URL:  "https://example.com/",
		}
		s.Equal(ErrModuleBadPath, m.Validate())
	}
}

func (s *ModulesSuite) TestValidateMissingName() {
	m := &Submodule{Name: "ok", URL: "bar"}
	s.Equal(ErrModuleEmptyPath, m.Validate())
}

func (s *ModulesSuite) TestValidateBadName() {
	input := []string{
		// Plain shapes the parser must reject regardless of OS.
		"",
		".",
		"..",
		"../x",
		"a/../../b",
		"/abs",
		`C:\win`,
		"x\x00y",
		"x/",
		"/x",
		`.\..\foo`,
		"modules/../escape",

		// HFS+ ignores certain Unicode code points during path
		// normalisation, so these all resolve to ".." on macOS.
		".\u200c.",             // ZWNJ between dots
		"\u200c..",             // leading ZWNJ
		"..\u200c",             // trailing ZWNJ
		"\u200c.\u200d.\u200e", // ZWNJ + ZWJ + LRM
		"a/.\u200c./b",         // hidden ".." mid-path

		// NTFS strips trailing spaces, dots, and an alternate-data
		// -stream suffix during canonicalisation, so these all
		// resolve to ".." on Windows.
		".. ",
		"..  ",
		"....",
		".. .",
		"..::$INDEX_ALLOCATION",
		"..:foo",
		"a/.. /b",
	}
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
