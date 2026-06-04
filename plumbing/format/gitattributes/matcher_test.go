package gitattributes

import (
	"strings"
)

func (s *MatcherSuite) TestMatcher_Match() {
	lines := []string{
		"[attr]binary -diff -merge -text",
		"**/middle/v[uo]l?ano binary text eol=crlf",
		"volcano -eol",
		"foobar diff merge text eol=lf foo=bar",
	}

	ma, err := ReadAttributes(strings.NewReader(strings.Join(lines, "\n")), nil, true)
	s.NoError(err)

	m := NewMatcher(ma)
	results, matched := m.Match([]string{"head", "middle", "vulkano"}, nil)

	s.True(matched)
	s.True(results["binary"].IsSet())
	s.True(results["diff"].IsUnset())
	s.True(results["merge"].IsUnset())
	s.True(results["text"].IsSet())
	s.Equal("crlf", results["eol"].Value())
}
