package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ModulesSuite struct {
	suite.Suite
}

func TestModulesSuite(t *testing.T) {
	suite.Run(t, new(ModulesSuite))
}

func (s *ModulesSuite) TestValidateMissingURL() {
	m := &Submodule{Path: "foo"}
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
			Path: p,
			URL:  "https://example.com/",
		}
		s.Equal(ErrModuleBadPath, m.Validate())
	}
}

func (s *ModulesSuite) TestValidateMissingName() {
	m := &Submodule{URL: "bar"}
	s.Equal(ErrModuleEmptyPath, m.Validate())
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
`)

	cfg := NewModules()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	s.Len(cfg.Submodules, 2)
	s.Equal("qux", cfg.Submodules["qux"].Name)
	s.Equal("https://github.com/foo/qux.git", cfg.Submodules["qux"].URL)
	s.Equal("foo/bar", cfg.Submodules["foo/bar"].Name)
	s.Equal("https://github.com/foo/bar.git", cfg.Submodules["foo/bar"].URL)
	s.Equal("dev", cfg.Submodules["foo/bar"].Branch)
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
