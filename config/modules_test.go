package config

import (
	"errors"

	. "gopkg.in/check.v1"
)

type ModulesSuite struct{}

var _ = Suite(&ModulesSuite{})

func (s *ModulesSuite) TestValidateMissingURL(c *C) {
	m := &Submodule{Name: "foo", Path: "foo"}
	c.Assert(m.Validate(), Equals, ErrModuleEmptyURL)
}

func (s *ModulesSuite) TestValidateBadPath(c *C) {
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
		c.Assert(m.Validate(), Equals, ErrModuleBadPath)
	}
}

func (s *ModulesSuite) TestValidateMissingName(c *C) {
	m := &Submodule{Name: "ok", URL: "bar"}
	c.Assert(m.Validate(), Equals, ErrModuleEmptyPath)
}

func (s *ModulesSuite) TestValidateBadName(c *C) {
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
		// name: <name>"), so use errors.Is.
		c.Assert(errors.Is(m.Validate(), ErrModuleBadName), Equals, true,
			Commentf("name %q", n))
	}
}

func (s *ModulesSuite) TestValidateGoodName(c *C) {
	for _, n := range []string{"foo", "lib-foo", "deps/x", "x.y"} {
		m := &Submodule{
			Name: n,
			Path: "ok",
			URL:  "https://example.com/",
		}
		c.Assert(m.Validate(), IsNil, Commentf("name %q", n))
	}
}

func (s *ModulesSuite) TestMarshal(c *C) {
	input := []byte(`[submodule "qux"]
	path = qux
	url = baz
	branch = bar
`)

	cfg := NewModules()
	cfg.Submodules["qux"] = &Submodule{Path: "qux", URL: "baz", Branch: "bar"}

	output, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, input)
}

func (s *ModulesSuite) TestUnmarshal(c *C) {
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
	c.Assert(err, IsNil)

	// The "suspicious" entry is dropped because of its `..` path,
	// and the `..` entry is dropped because of its suspicious name
	// (canonical Git's "ignoring suspicious submodule name" rule).
	c.Assert(cfg.Submodules, HasLen, 2)
	c.Assert(cfg.Submodules["qux"].Name, Equals, "qux")
	c.Assert(cfg.Submodules["qux"].URL, Equals, "https://github.com/foo/qux.git")
	c.Assert(cfg.Submodules["foo/bar"].Name, Equals, "foo/bar")
	c.Assert(cfg.Submodules["foo/bar"].URL, Equals, "https://github.com/foo/bar.git")
	c.Assert(cfg.Submodules["foo/bar"].Branch, Equals, "dev")
	_, hasDotDot := cfg.Submodules[".."]
	c.Assert(hasDotDot, Equals, false)
}

func (s *ModulesSuite) TestUnmarshalMarshal(c *C) {
	input := []byte(`[submodule "foo/bar"]
	path = foo/bar
	url = https://github.com/foo/bar.git
	ignore = all
`)

	cfg := NewModules()
	err := cfg.Unmarshal(input)
	c.Assert(err, IsNil)

	output, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(string(output), DeepEquals, string(input))
}
