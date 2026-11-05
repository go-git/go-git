package gitattributes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Each case is checked against `git check-attr text binary diff` with Git
// 2.54: the matching line of the highest priority decides each attribute,
// and a set macro fills only attributes still undetermined.
func TestMatcherMatchesGitPrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// root, nested and info are the root .gitattributes, d/.gitattributes
		// and info/attributes.
		root, nested, info string
		path               string
		// want holds the states of text, binary and diff, "" when
		// unspecified.
		want [3]string
	}{
		{name: "-text after text=auto", root: "* text=auto\n*.dat -text -diff\n", path: "x.dat", want: [3]string{"unset", "", "unset"}},
		{name: "LFS after text=auto", root: "* text=auto\n*.dat filter=lfs diff=lfs merge=lfs -text\n", path: "x.dat", want: [3]string{"unset", "", "lfs"}},
		{name: "other attribute after -text", root: "*.dat -text\n*.dat linguist-generated\n", path: "x.dat", want: [3]string{"unset", "", ""}},
		{name: "custom macro", root: "[attr]keep -text\n* text\n*.dat keep\n", path: "x.dat", want: [3]string{"unset", "", ""}},
		{name: "nested line without text", root: "*.dat -text\n", nested: "*.dat eol=lf\n", path: "d/x.dat", want: [3]string{"unset", "", ""}},
		{name: "later line for another attribute", root: "d/s -text\nd/* diff\n", path: "d/s", want: [3]string{"unset", "", "set"}},
		{name: "nested overrides root", root: "d/s text\n", nested: "s -text -diff\n", path: "d/s", want: [3]string{"unset", "", "unset"}},
		{name: "info overrides root", root: "d/s text\n", info: "d/s -text -diff\n", path: "d/s", want: [3]string{"unset", "", "unset"}},
		{name: "binary macro in nested file", root: "d/s text\n", nested: "s binary -diff\n", path: "d/s", want: [3]string{"unset", "set", "unset"}},
		{name: "text after binary on a line", root: "*.dat binary text\n", path: "x.dat", want: [3]string{"set", "set", "unset"}},
		{name: "binary after -binary", root: "*.dat -binary\n*.dat binary\n", path: "x.dat", want: [3]string{"unset", "set", "unset"}},
		{name: "quoted empty pattern", root: "\"\" text\n*.dat -text\n", path: "x.dat", want: [3]string{"unset", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stack []MatchAttribute
			for _, f := range []struct {
				content string
				domain  []string
			}{{tc.root, nil}, {tc.nested, []string{"d"}}, {tc.info, nil}} {
				attrs, err := ReadAttributes(strings.NewReader(f.content), f.domain, f.domain == nil)
				require.NoError(t, err)
				stack = append(stack, attrs...)
			}

			m := NewMatcher(stack)
			path := strings.Split(tc.path, "/")
			state := func(a Attribute) string {
				switch {
				case a == nil, a.IsUnspecified():
					return ""
				case a.IsSet():
					return "set"
				case a.IsUnset():
					return "unset"
				default:
					return a.Value()
				}
			}

			all, _ := m.Match(path, nil)
			var got [3]string
			for i, name := range []string{"text", "binary", "diff"} {
				// Asking for one attribute, or for all of them, gives the
				// same state.
				one, _ := m.Match(path, []string{name})
				got[i] = state(one[name])
				assert.Equal(t, got[i], state(all[name]), name)
			}
			assert.Equal(t, tc.want, got)

			three, _ := m.Match(path, []string{"text", "binary", "diff"})
			for i, name := range []string{"text", "binary", "diff"} {
				assert.Equal(t, tc.want[i], state(three[name]), name)
			}
		})
	}
}
