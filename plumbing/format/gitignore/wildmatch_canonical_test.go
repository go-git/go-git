package gitignore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// WildmatchCanonicalSuite contains tests based on git's wildmatch test suite (t3070-wildmatch.sh)
// These tests focus on the core pattern matching functionality that gitignore relies on
type WildmatchCanonicalSuite struct {
	suite.Suite
}

func TestWildmatchCanonicalSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(WildmatchCanonicalSuite))
}

// Helper to test single pattern against single path
func (s *WildmatchCanonicalSuite) testPattern(pattern, text string, expected bool, desc string) {
	p := ParsePattern(pattern, nil)

	// Detect if text represents a directory (has trailing slash)
	isDir := strings.HasSuffix(text, "/")

	pathSlice := strings.Split(strings.Trim(text, "/"), "/")
	if pathSlice[0] == "" {
		pathSlice = []string{}
	}
	result := p.Match(pathSlice, isDir) == Exclude
	s.Equal(expected, result, "%s: pattern=%q text=%q", desc, pattern, text)
}

// Test basic wildmatch features from git
func (s *WildmatchCanonicalSuite) TestBasicWildmatch() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		{"foo", "foo", true, "exact match"},
		{"foo", "bar", false, "no match"},
		{"", "", false, "empty pattern doesn't match"},
		{"???", "foo", true, "three question marks"},
		{"??", "foo", false, "two question marks vs three chars"},
		{"*", "foo", true, "star matches anything"},
		{"f*", "foo", true, "prefix star"},
		{"*f", "foo", false, "suffix star doesn't match"},
		{"*foo*", "foo", true, "surrounding stars"},
		{"*ob*a*r*", "foobar", true, "multiple stars"},
		{"*ab", "aaaaaaabababab", true, "star with suffix"},
		{"foo\\*", "foo*", true, "escaped star"},
		{"foo\\*bar", "foobar", false, "escaped star no match"},
		{"f\\\\oo", "f\\oo", true, "escaped backslash"},
	}

	for _, tt := range tests {
		s.testPattern(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

// Test bracket expressions - the critical compatibility issue
func (s *WildmatchCanonicalSuite) TestBracketExpressions() {
	tests := []struct {
		pattern     string
		text        string
		expected    bool
		gitExpected bool // What git actually does
		desc        string
	}{
		// Character sets
		{"*[al]?", "ball", true, true, "character set [al]"},
		{"[ten]", "ten", false, false, "literal character match"},
		{"**[!te]", "ten", false, true, "negated character set [!te] - DISCREPANCY"},
		{"**[!ten]", "ten", false, false, "negated character set [!ten]"},
		{"t[a-g]n", "ten", true, true, "character range [a-g]"},
		{"t[!a-g]n", "ten", true, false, "negated range [!a-g] - DISCREPANCY"},
		{"t[!a-g]n", "ton", true, true, "negated range [!a-g] should match o"},
		{"t[^a-g]n", "ton", true, true, "caret negation [^a-g]"},

		// Bracket edge cases
		{"a[]]b", "a]b", true, true, "closing bracket in set"},
		{"a[]-]b", "a-b", true, true, "dash in bracket set"},
		{"a[]-]b", "a]b", true, true, "bracket and dash"},
		{"a[]-]b", "aab", false, false, "bracket set no match"},
		{"a[]a-]b", "aab", true, true, "complex bracket set"},
		{"]", "]", true, true, "literal bracket"},
	}

	for _, tt := range tests {
		// Use gitExpected as the canonical expectation since we want to match Git's behavior
		expected := tt.gitExpected
		s.testPattern(tt.pattern, tt.text, expected, tt.desc)
	}
}

// Test extended slash-matching features (double star patterns)
func (s *WildmatchCanonicalSuite) TestDoubleStarPatterns() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Basic double star
		{"foo*bar", "foo/baz/bar", false, "single star doesn't cross slashes"},
		{"foo**bar", "foo/baz/bar", false, "double star without slashes"},
		{"foo**bar", "foobazbar", true, "double star matches within segment"},
		{"foo/**/bar", "foo/baz/bar", true, "double star with slashes"},
		{"foo/**/**/bar", "foo/baz/bar", true, "multiple double stars"},
		{"foo/**/bar", "foo/b/a/z/bar", true, "double star multiple levels"},
		{"foo/**/**/bar", "foo/b/a/z/bar", true, "multiple double stars multiple levels"},
		{"foo/**/bar", "foo/bar", true, "double star zero levels"},
		{"foo/**/**/bar", "foo/bar", true, "multiple double stars zero levels"},

		// Double star from root
		{"**/foo", "foo", true, "double star from root matches"},
		{"**/foo", "XXX/foo", true, "double star matches nested"},
		{"**/foo", "bar/baz/foo", true, "double star matches deep nested"},
		{"*/foo", "bar/baz/foo", false, "single star doesn't match deep"},

		// Double star in middle
		{"**/bar*", "foo/bar/baz", true, "double star with suffix"},
		{"**/bar/*", "deep/foo/bar/baz", true, "double star with path continuation"},
		{"**/bar/*", "deep/foo/bar/baz/", true, "double star path with trailing slash"},
		{"**/bar/**", "deep/foo/bar/baz/", true, "double star with double star"},
		{"**/bar/*", "deep/foo/bar", false, "double star needs continuation"},
		{"**/bar/**", "deep/foo/bar/", true, "double star matches dir"},

		// Complex patterns
		{"*/bar/**", "foo/bar/baz/x", true, "mixed single/double star"},
		{"*/bar/**", "deep/foo/bar/baz/x", false, "mixed star scope"},
		{"**/bar/*/*", "deep/foo/bar/baz/x", true, "double star with specific depth"},
	}

	for _, tt := range tests {
		s.testPattern(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

// Test character class patterns
func (s *WildmatchCanonicalSuite) TestCharacterClasses() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// POSIX character classes
		{"[[:alpha:]][[:digit:]][[:upper:]]", "a1B", true, "alpha/digit/upper classes"},
		{"[[:digit:][:upper:][:space:]]", "A", true, "multiple classes - upper"},
		{"[[:digit:][:upper:][:space:]]", "1", true, "multiple classes - digit"},
		{"[[:digit:][:upper:][:space:]]", " ", true, "multiple classes - space"},
		{"[[:digit:][:upper:][:space:]]", ".", false, "classes don't match period"},
		{"[[:digit:][:punct:][:space:]]", ".", true, "punct class matches period"},
		{"[[:xdigit:]]", "5", true, "hex digit - number"},
		{"[[:xdigit:]]", "f", true, "hex digit - letter"},
		{"[[:xdigit:]]", "D", true, "hex digit - upper"},
		{"[[:xdigit:]]", "g", false, "non-hex letter"},

		// Character class negation
		{"[^[:alnum:][:alpha:][:blank:][:cntrl:][:digit:][:lower:][:space:][:upper:][:xdigit:]]", ".", true, "negated classes"},
		{"[a-c[:digit:]x-z]", "5", true, "mixed range and class"},
		{"[a-c[:digit:]x-z]", "b", true, "mixed range match"},
		{"[a-c[:digit:]x-z]", "y", true, "mixed range end"},
		{"[a-c[:digit:]x-z]", "q", false, "mixed range no match"},
	}

	for _, tt := range tests {
		s.testPattern(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

// Test malformed and edge case patterns
//
//nolint:dupl // Table-driven test structure similar to TestExtendedSlashMatching but tests different behavior
func (s *WildmatchCanonicalSuite) TestMalformedPatterns() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Bracket edge cases
		{"[\\-^]", "]", false, "backslash dash caret doesn't match ]"},
		{"[\\-^]", "[", false, "bracket doesn't match set"},
		{"[\\-^]", "-", true, "dash matches escaped dash range"},
		{"[\\]]", "]", true, "escaped closing bracket"},
		{"[\\]]", "\\]", false, "literal vs escaped"},

		// Empty and malformed brackets
		{"a[]b", "ab", false, "empty bracket set"},
		{"a[]b", "a[]b", false, "empty brackets are invalid"},
		{"ab[", "ab[", false, "unclosed bracket is invalid"},
		{"[!", "ab", false, "incomplete negation"},
		{"[-", "ab", false, "incomplete range"},
		{"[-]", "-", true, "dash only"},

		// Range edge cases
		{"[a-", "-", false, "incomplete range"},
		{"[!a-", "-", false, "incomplete negated range"},
		{"[--A]", "-", true, "dash to letter range"},
		{"[--A]", "5", true, "dash range includes numbers"},
		{"[ --]", " ", true, "space to dash range"},
		{"[ --]", "$", true, "space range includes symbols"},
		{"[ --]", "-", true, "space range includes dash"},
		{"[ --]", "0", false, "space range doesn't include digits"},

		// Multiple dashes
		{"[---]", "-", true, "triple dash"},
		{"[------]", "-", true, "many dashes"},

		// Complex malformed
		{"[a-e-n]", "j", false, "invalid double range"},
		{"[a-e-n]", "-", true, "dash matches in invalid range"},
		{"[!------]", "a", true, "negated many dashes"},

		// Backslash handling
		{"[\\]", "\\", false, "single backslash in brackets"},
		{"[\\\\]", "\\", true, "escaped backslash in brackets"},
		{"[!\\\\]", "\\", false, "negated backslash"},
		{"[A-\\\\]", "G", true, "range to backslash"},
	}

	for _, tt := range tests {
		s.testPattern(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

// Test complex recursion patterns
func (s *WildmatchCanonicalSuite) TestRecursionPatterns() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Font name pattern (classic recursion test)
		{
			"-*-*-*-*-*-*-12-*-*-*-m-*-*-*",
			"-adobe-courier-bold-o-normal--12-120-75-75-m-70-iso8859-1",
			true, "complex font pattern match",
		},
		{
			"-*-*-*-*-*-*-12-*-*-*-m-*-*-*",
			"-adobe-courier-bold-o-normal--12-120-75-75-X-70-iso8859-1",
			false, "font pattern with wrong char",
		},

		// Path pattern recursion
		{
			"XXX/*/*/*/*/*/*/12/*/*/*/m/*/*/*",
			"XXX/adobe/courier/bold/o/normal//12/120/75/75/m/70/iso8859/1",
			true, "deep path pattern",
		},
		{
			"XXX/*/*/*/*/*/*/12/*/*/*/m/*/*/*",
			"XXX/adobe/courier/bold/o/normal//12/120/75/75/X/70/iso8859/1",
			false, "deep path pattern mismatch",
		},

		// Complex double star
		{
			"**/*a*b*g*n*t",
			"abcd/abcdefg/abcdefghijk/abcdefghijklmnop.txt",
			true, "complex scattered pattern",
		},
		{
			"**/*a*b*g*n*t",
			"abcd/abcdefg/abcdefghijk/abcdefghijklmnop.txtz",
			false, "complex pattern with extra chars",
		},

		// Multiple level patterns
		{"*/*/*", "foo/bb/aa", true, "three level pattern exact match"},
		{"*/*/*", "foo/bba/arr", true, "three level pattern exact match"},
		{"**/**/**", "foo/bb/aa/rr", true, "triple double star"},

		// Mixed patterns
		{"*X*i", "abcXdefXghi", true, "multiple X pattern"},
		{"*/*X*/*/*i", "ab/cXd/efXg/hi", true, "complex mixed pattern"},
		{"**/*X*/**/*i", "ab/cXd/efXg/hi", true, "double star mixed pattern"},
	}

	for _, tt := range tests {
		s.testPattern(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

// Test case sensitivity (for future case-insensitive support)
func (s *WildmatchCanonicalSuite) TestCaseSensitivity() {
	tests := []struct {
		pattern             string
		text                string
		expectedSensitive   bool
		expectedInsensitive bool
		desc                string
	}{
		{"[A-Z]", "a", false, true, "case range sensitivity"},
		{"[A-Z]", "A", true, true, "case range match upper"},
		{"[a-z]", "A", false, true, "lowercase range vs upper"},
		{"[a-z]", "a", true, true, "lowercase range match"},
		{"[[:upper:]]", "a", false, true, "upper class vs lower"},
		{"[[:upper:]]", "A", true, true, "upper class match"},
		{"[[:lower:]]", "A", false, true, "lower class vs upper"},
		{"[[:lower:]]", "a", true, true, "lower class match"},
		{"[B-Za]", "A", false, true, "mixed case range"},
		{"[B-a]", "A", false, true, "reverse case range"},
	}

	for _, tt := range tests {
		// Test case-sensitive behavior (current go-git)
		s.testPattern(tt.pattern, tt.text, tt.expectedSensitive, tt.desc+" (case-sensitive)")

		// Document what case-insensitive should do
		if tt.expectedSensitive != tt.expectedInsensitive {
			s.T().Logf("Case-insensitive would differ: %s vs %s - sensitive: %v, insensitive: %v",
				tt.pattern, tt.text, tt.expectedSensitive, tt.expectedInsensitive)
		}
	}
}
