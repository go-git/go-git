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
// 2.54 (an empty want means every attribute is unspecified): the matching line of the highest priority decides each attribute,
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
		{name: "directory pattern and a file below it", root: "docs/ -text\n", path: "docs/x"},
		{name: "directory pattern and a file of its name", root: "docs/ -text\n", path: "docs"},
		{name: "unset with a value", root: "* text\n*.dat -text=x\n", path: "x.dat", want: [3]string{"unset", "", ""}},
		{name: "unspecified with a value", root: "*.dat -text\n*.dat !text=x\n", path: "x.dat"},
		{name: "no-break space in a pattern", root: "z\u00a0q.bin -text\n", path: "z\u00a0q.bin", want: [3]string{"unset", "", ""}},
		{name: "no-break space is not a separator", root: "z\u00a0q.bin -text\n", path: "z"},
		{name: "vertical tab in a pattern", root: "v\vq.bin -text\n", path: "v\vq.bin", want: [3]string{"unset", "", ""}},
		{name: "quoted tab escape", root: "\"a\\tb\" -text\n", path: "a\tb", want: [3]string{"unset", "", ""}},
		{name: "quoted space", root: "\"sp ace\" -text\n", path: "sp ace", want: [3]string{"unset", "", ""}},
		{name: "quoted octal escapes", root: "\"\\303\\251.txt\" -text\n", path: "\u00e9.txt", want: [3]string{"unset", "", ""}},
		{name: "unterminated quote", root: "\"un terminated -text\n", path: "\"un", want: [3]string{"unset", "", ""}},
		{name: "attribute right after the quote", root: "\"x\"-text\n", path: "x", want: [3]string{"unset", "", ""}},
		{name: "tab separator and CR", root: "*.dat\t-text\r\n", path: "x.dat", want: [3]string{"unset", "", ""}},
		{name: "macro prefix alone", root: "[attr]\n", path: "x"},
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

// A pattern ending in a slash names a directory, so it matches no file,
// at any depth, and does not panic once the path outruns it.
func TestPatternWithTrailingSlashMatchesNoFile(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"docs/", "/docs/", "a/docs/", "/"} {
		for _, path := range [][]string{{"docs"}, {"docs", "x"}, {"docs", "x", "y"}, {"a", "docs", "x"}} {
			assert.False(t, ParsePattern(p, nil).Match(path), "%q %q", p, path)
		}
	}
}

// As in Git's parse_attr, a "-" or "!" prefix keeps its state and drops a
// value, and the name is checked up to the "=".
func TestParseAttributesLineDropsTheValueOfAPrefixedAttribute(t *testing.T) {
	t.Parallel()

	m, err := ParseAttributesLine("*.dat -text=x !eol=lf diff=lfs", nil, false)
	require.NoError(t, err)
	require.Len(t, m.Attributes, 3)
	assert.True(t, m.Attributes[0].IsUnset())
	assert.Equal(t, "text", m.Attributes[0].Name())
	assert.Empty(t, m.Attributes[0].Value())
	assert.True(t, m.Attributes[1].IsUnspecified())
	assert.Equal(t, "eol", m.Attributes[1].Name())
	assert.True(t, m.Attributes[2].IsValueSet())
	assert.Equal(t, "lfs", m.Attributes[2].Value())

	_, err = ParseAttributesLine("*.dat -te@xt=x", nil, false)
	assert.ErrorIs(t, err, ErrInvalidAttributeName)
}
