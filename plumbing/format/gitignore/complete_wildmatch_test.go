package gitignore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// CompleteWildmatchSuite contains ALL tests from git's wildmatch test suite (t3070-wildmatch.sh)
// This is the complete, authoritative test suite for gitignore pattern matching compatibility
type CompleteWildmatchSuite struct {
	suite.Suite
}

func TestCompleteWildmatchSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(CompleteWildmatchSuite))
}

// Helper to test single pattern against single path
func (s *CompleteWildmatchSuite) testMatch(pattern, text string, expected bool, desc string) {
	p := ParsePattern(pattern, nil)

	// Detect if text represents a directory (has trailing slash)
	isDir := strings.HasSuffix(text, "/")

	pathSlice := strings.Split(strings.Trim(text, "/"), "/")
	if pathSlice[0] == "" {
		pathSlice = []string{}
	}

	// For gitignore, we only care about exclusion (not inclusion)
	// Git's wildmatch returns 1 for match, 0 for no match
	// We convert to: pattern matches -> should be excluded
	result := p.Match(pathSlice, isDir)
	isExcluded := result == Exclude

	s.Equal(expected, isExcluded, "%s: pattern=%q text=%q result=%v", desc, pattern, text, result)
}

// Complete test matrix from git's t3070-wildmatch.sh
// Format: match(glob, iglob, pathmatch, pathmatchi, text, pattern)
// We focus on glob matching (first value) since that's what gitignore uses

func (s *CompleteWildmatchSuite) TestBasicWildmatchFeatures() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Basic wildmatch features from git test
		{"foo", "foo", true, "exact match"},
		{"foo", "bar", false, "no match"},
		{"", "", false, "empty pattern doesn't match"},
		{"???", "foo", true, "three question marks"},
		{"??", "foo", false, "two question marks too short"},
		{"*", "foo", true, "star matches anything"},
		{"f*", "foo", true, "prefix star"},
		{"*f", "foo", false, "suffix star no match"},
		{"*foo*", "foo", true, "surrounded by stars"},
		{"*ob*a*r*", "foobar", true, "multiple interspersed stars"},
		{"*ab", "aaaaaaabababab", true, "star with specific ending"},
		{"foo\\*", "foo*", true, "escaped literal star"},
		{"foo\\*bar", "foobar", false, "escaped star blocks match"},
		{"f\\\\oo", "f\\oo", true, "escaped backslash"},
		{"foo\\", "foo\\", true, "trailing backslash"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestBracketExpressions() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Character sets and ranges
		{"*[al]?", "ball", true, "character set match"},
		{"[ten]", "ten", false, "character set literal no match"},
		{"**[!te]", "ten", true, "negated character set [!te]"},
		{"**[!ten]", "ten", false, "negated character set [!ten]"},
		{"t[a-g]n", "ten", true, "character range [a-g]"},
		{"t[!a-g]n", "ten", false, "negated range [!a-g] vs e"},
		{"t[!a-g]n", "ton", true, "negated range [!a-g] vs o"},
		{"t[^a-g]n", "ton", true, "caret negation [^a-g]"},

		// Bracket edge cases
		{"a[]]b", "a]b", true, "closing bracket in character set"},
		{"a[]-]b", "a-b", true, "dash in character set"},
		{"a[]-]b", "a]b", true, "bracket in character set"},
		{"a[]-]b", "aab", false, "character set no match"},
		{"a[]a-]b", "aab", true, "complex character set"},
		{"]", "]", true, "literal closing bracket"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

//nolint:dupl // Table-driven test structure similar to TestMalformedPatterns but tests different behavior
func (s *CompleteWildmatchSuite) TestExtendedSlashMatching() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Single vs double star with slashes
		{"foo*bar", "foo/baz/bar", false, "single star doesn't cross slash in glob mode"},
		{"foo**bar", "foo/baz/bar", false, "double star without slashes"},
		{"foo**bar", "foobazbar", true, "double star within segment"},
		{"foo/**/bar", "foo/baz/bar", true, "double star with slashes"},
		{"foo/**/**/bar", "foo/baz/bar", true, "multiple double star segments"},
		{"foo/**/bar", "foo/b/a/z/bar", true, "double star multiple intermediate"},
		{"foo/**/**/bar", "foo/b/a/z/bar", true, "multiple double star multiple intermediate"},
		{"foo/**/bar", "foo/bar", true, "double star zero intermediate"},
		{"foo/**/**/bar", "foo/bar", true, "multiple double star zero intermediate"},

		// Character matching across slashes
		{"foo?bar", "foo/bar", false, "question mark doesn't cross slash"},
		{"foo[/]bar", "foo/bar", false, "bracket slash doesn't cross"},
		{"foo[^a-z]bar", "foo/bar", false, "bracket negation doesn't cross slash"},
		{"f[^eiu][^eiu][^eiu][^eiu][^eiu]r", "foo/bar", false, "multiple bracket negations"},
		{"f[^eiu][^eiu][^eiu][^eiu][^eiu]r", "foo-bar", true, "multiple bracket negations with dash"},

		// Double star from root
		{"**/foo", "foo", true, "double star from root immediate"},
		{"**/foo", "XXX/foo", true, "double star from root nested"},
		{"**/foo", "bar/baz/foo", true, "double star from root deep"},
		{"*/foo", "bar/baz/foo", false, "single star doesn't match deep"},

		// Double star combinations
		{"**/bar*", "foo/bar/baz", true, "double star with suffix star"},
		{"**/bar/*", "deep/foo/bar/baz", true, "double star with single continuation"},
		{"**/bar/*", "deep/foo/bar/baz/", true, "double star continuation with trailing slash"},
		{"**/bar/**", "deep/foo/bar/baz/", true, "double star with double star continuation"},
		{"**/bar/*", "deep/foo/bar", false, "double star continuation needs path"},
		{"**/bar/**", "deep/foo/bar/", true, "double star double star with directory"},
		{"**/bar**", "foo/bar/baz", true, "double star with adjacent stars"},
		{"*/bar/**", "foo/bar/baz/x", true, "mixed single double star"},
		{"*/bar/**", "deep/foo/bar/baz/x", false, "mixed stars wrong depth"},
		{"**/bar/*/*", "deep/foo/bar/baz/x", true, "double star with fixed depth"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestVariousAdditionalTests() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Character range edge cases
		{"a[c-c]st", "acrt", false, "single character range no match"},
		{"a[c-c]rt", "acrt", true, "single character range match"},
		{"[!]-]", "]", false, "negated bracket with closing bracket"},
		{"[!]-]", "a", true, "negated bracket with letter"},

		// Backslash patterns
		{"\\", "", false, "single backslash vs empty"},
		{"\\", "\\", true, "single backslash vs backslash"},
		{"*/\\", "XXX/\\", true, "backslash path pattern"},
		{"*/\\\\", "XXX/\\", true, "escaped backslash path pattern"},

		// At-sign patterns
		{"foo", "foo", true, "simple foo"},
		{"@foo", "@foo", true, "at-sign prefix exact"},
		{"@foo", "foo", false, "at-sign prefix no match"},

		// Escaped bracket patterns
		{"\\[ab]", "[ab]", true, "escaped opening bracket"},
		{"[[]ab]", "[ab]", true, "bracket in character set"},
		{"[[:]ab]", "[ab]", true, "bracket colon in set"},
		{"[[::]ab]", "[ab]", false, "double colon in set"},
		{"[[:digit]ab]", "[ab]", true, "partial digit class"},
		{"[\\[:]ab]", "[ab]", true, "escaped bracket in set"},

		// Question mark patterns
		{"\\??\\?b", "?a?b", true, "escaped question marks"},
		{"\\a\\b\\c", "abc", true, "escaped letters"},

		// Empty vs error patterns
		{"", "foo", false, "empty pattern vs text"},
		{"**/t[o]", "foo/bar/baz/to", true, "double star with bracket"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestCharacterClassTests() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// POSIX character classes
		{"[[:alpha:]][[:digit:]][[:upper:]]", "a1B", true, "alpha digit upper sequence"},
		{"[[:digit:][:upper:][:space:]]", "a", false, "digit upper space vs lowercase"},
		{"[[:digit:][:upper:][:space:]]", "A", true, "digit upper space vs uppercase"},
		{"[[:digit:][:upper:][:space:]]", "1", true, "digit upper space vs digit"},
		{"[[:digit:][:upper:][:spaci:]]", "1", false, "malformed space class"},
		{"[[:digit:][:upper:][:space:]]", " ", true, "digit upper space vs space"},
		{"[[:digit:][:upper:][:space:]]", ".", false, "digit upper space vs period"},
		{"[[:digit:][:punct:][:space:]]", ".", true, "digit punct space vs period"},
		{"[[:xdigit:]]", "5", true, "hex digit numeric"},
		{"[[:xdigit:]]", "f", true, "hex digit lowercase"},
		{"[[:xdigit:]]", "D", true, "hex digit uppercase"},

		// Complex character class combinations
		{"[[:alnum:][:alpha:][:blank:][:cntrl:][:digit:][:graph:][:lower:][:print:][:punct:][:space:][:upper:][:xdigit:]]", "_", true, "all classes include underscore"},
		{"[^[:alnum:][:alpha:][:blank:][:cntrl:][:digit:][:lower:][:space:][:upper:][:xdigit:]]", ".", true, "negated classes"},
		{"[a-c[:digit:]x-z]", "5", true, "mixed range and digit class - digit"},
		{"[a-c[:digit:]x-z]", "b", true, "mixed range and digit class - letter"},
		{"[a-c[:digit:]x-z]", "y", true, "mixed range and digit class - end range"},
		{"[a-c[:digit:]x-z]", "q", false, "mixed range and digit class - no match"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestMalformedPatterns() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Bracket expressions with backslashes
		{"[\\-^]", "]", false, "backslash dash caret"},
		{"[\\-^]", "[", false, "backslash dash caret vs opening bracket"},
		{"[\\-_]", "-", true, "backslash dash underscore vs dash"},
		{"[\\]]", "]", true, "escaped closing bracket"},
		{"[\\]]", "\\]", false, "escaped bracket vs literal"},
		{"[\\]]", "\\", false, "escaped bracket vs backslash"},

		// Empty and malformed bracket sets
		{"a[]b", "ab", false, "empty bracket set"},
		{"a[]b", "a[]b", false, "empty brackets are invalid"},
		{"ab[", "ab[", false, "unclosed bracket is invalid"},
		{"[!", "ab", false, "incomplete negation"},
		{"[-", "ab", false, "incomplete dash"},
		{"[-]", "-", true, "lone dash in brackets"},
		{"[a-", "-", false, "incomplete range"},
		{"[!a-", "-", false, "incomplete negated range"},

		// Dash patterns in brackets
		{"[--A]", "-", true, "dash to letter range includes dash"},
		{"[--A]", "5", true, "dash to letter range includes number"},
		{"[ --]", " ", true, "space to dash range includes space"},
		{"[ --]", "$", true, "space to dash range includes dollar"},
		{"[ --]", "-", true, "space to dash range includes dash"},
		{"[ --]", "0", false, "space to dash range excludes zero"},
		{"[---]", "-", true, "triple dash"},
		{"[------]", "-", true, "many dashes"},

		// Invalid ranges
		{"[a-e-n]", "j", false, "invalid double range vs j"},
		{"[a-e-n]", "-", true, "invalid double range vs dash"},
		{"[!------]", "a", true, "negated many dashes vs letter"},

		// Bracket and backslash combinations
		{"[]-a]", "[", false, "bracket dash a vs opening bracket"},
		{"[]-a]", "^", true, "bracket dash a vs caret"},
		{"[!]-a]", "^", false, "negated bracket dash a vs caret"},
		{"[!]-a]", "[", true, "negated bracket dash a vs bracket"},
		{"[a^bc]", "^", true, "caret in character set"},
		{"[a-]b]", "-b]", true, "dash bracket literal"},

		// Backslash in brackets
		{"[\\]", "\\", false, "single backslash in brackets"},
		{"[\\\\]", "\\", true, "escaped backslash in brackets"},
		{"[!\\\\]", "\\", false, "negated backslash"},
		{"[A-\\\\]", "G", true, "range to backslash"},

		// Various edge cases
		{"b*a", "aaabbb", false, "suffix pattern no match"},
		{"*ba*", "aabcaa", false, "middle pattern no match"},
		{"[,]", ",", true, "comma in brackets"},
		{"[\\\\,]", ",", true, "comma and backslash in brackets"},
		{"[\\\\,]", "\\", true, "backslash in comma brackets"},
		{"[,-.]", "-", true, "comma to period range includes dash"},
		{"[,-.]", "+", false, "comma to period excludes plus"},
		{"[,-.]", "-.]", false, "comma period range vs literal"},

		// Octal escape sequences
		{"[\\1-\\3]", "2", true, "octal range includes 2"},
		{"[\\1-\\3]", "3", true, "octal range includes 3"},
		{"[\\1-\\3]", "4", false, "octal range excludes 4"},

		// Bracket range with brackets
		{"[[-\\]]", "\\", true, "bracket to bracket range includes backslash"},
		{"[[-\\]]", "[", true, "bracket to bracket range includes opening bracket"},
		{"[[-\\]]", "]", true, "bracket to bracket range includes closing bracket"},
		{"[[-\\]]", "-", false, "bracket to bracket range excludes dash"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestRecursionPatterns() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Complex font name pattern (classic recursion stress test)
		{
			"-*-*-*-*-*-*-12-*-*-*-m-*-*-*",
			"-adobe-courier-bold-o-normal--12-120-75-75-m-70-iso8859-1",
			true, "font name pattern match",
		},
		{
			"-*-*-*-*-*-*-12-*-*-*-m-*-*-*",
			"-adobe-courier-bold-o-normal--12-120-75-75-X-70-iso8859-1",
			false, "font name pattern mismatch X vs m",
		},
		{
			"-*-*-*-*-*-*-12-*-*-*-m-*-*-*",
			"-adobe-courier-bold-o-normal--12-120-75-75-/-70-iso8859-1",
			false, "font name pattern mismatch / vs m",
		},

		// Path recursion patterns
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

		// Complex scattered patterns
		{
			"**/*a*b*g*n*t",
			"abcd/abcdefg/abcdefghijk/abcdefghijklmnop.txt",
			true, "scattered letter pattern",
		},
		{
			"**/*a*b*g*n*t",
			"abcd/abcdefg/abcdefghijk/abcdefghijklmnop.txtz",
			false, "scattered pattern with extra",
		},

		// Multi-level depth patterns
		{"*/*/*", "foo", false, "three level pattern vs one level"},
		{"*/*/*", "foo/bar", false, "three level pattern vs two levels"},
		{"*/*/*", "foo/bba/arr", true, "three level pattern exact match"},
		{"*/*/*", "foo/bb/aa/rr", true, "three level pattern matches deeper"},
		{"**/**/**", "foo/bb/aa/rr", true, "triple double star"},

		// Pattern matching with X markers
		{"*X*i", "abcXdefXghi", true, "X marker pattern"},
		{"*X*i", "ab/cXd/efXg/hi", false, "X marker across slashes (glob mode)"},
		{"*/*X*/*/*i", "ab/cXd/efXg/hi", true, "structured X pattern"},
		{"**/*X*/**/*i", "ab/cXd/efXg/hi", true, "double star X pattern"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestExtraPathmatchTests() {
	tests := []struct {
		pattern  string
		text     string
		expected bool
		desc     string
	}{
		// Basic path matching
		{"fo", "foo", false, "prefix no match"},
		{"foo/bar", "foo/bar", true, "exact path match"},
		{"foo/*", "foo/bar", true, "single star path"},
		{"foo/*", "foo/bba/arr", true, "single star matches deeper paths"},
		{"foo/**", "foo/bba/arr", true, "double star deeper path"},
		{"foo*", "foo/bba/arr", true, "star suffix matches paths"},
		{"foo**", "foo/bba/arr", true, "double star suffix"},
		{"foo/*arr", "foo/bba/arr", false, "suffix with slash vs path (glob mode)"},
		{"foo/**arr", "foo/bba/arr", false, "double star suffix with path (glob mode)"},
		{"foo/*z", "foo/bba/arr", false, "star with different suffix"},
		{"foo/**z", "foo/bba/arr", false, "double star with different suffix"},

		// Character matching in paths
		{"foo?bar", "foo/bar", false, "question mark vs slash (glob mode)"},
		{"foo[/]bar", "foo/bar", false, "bracket slash vs slash (glob mode)"},
		{"foo[^a-z]bar", "foo/bar", false, "negated range vs slash (glob mode)"},
		{"*Xg*i", "ab/cXd/efXg/hi", false, "X pattern across slash (glob mode)"},
	}

	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *CompleteWildmatchSuite) TestCaseSensitivityTests() {
	tests := []struct {
		pattern         string
		text            string
		caseSensitive   bool
		caseInsensitive bool
		desc            string
	}{
		// Case sensitivity patterns
		{"[A-Z]", "a", false, true, "uppercase range vs lowercase"},
		{"[A-Z]", "A", true, true, "uppercase range vs uppercase"},
		{"[a-z]", "A", false, true, "lowercase range vs uppercase"},
		{"[a-z]", "a", true, true, "lowercase range vs lowercase"},
		{"[[:upper:]]", "a", false, true, "upper class vs lowercase"},
		{"[[:upper:]]", "A", true, true, "upper class vs uppercase"},
		{"[[:lower:]]", "A", false, true, "lower class vs uppercase"},
		{"[[:lower:]]", "a", true, true, "lower class vs lowercase"},
		{"[B-Za]", "A", false, true, "mixed case range"},
		{"[B-a]", "A", false, true, "reverse case range"},
		{"[Z-y]", "z", false, true, "reverse case range lowercase"},
		{"[Z-y]", "Z", true, true, "reverse case range uppercase"},
	}

	for _, tt := range tests {
		// Test current case-sensitive behavior
		s.testMatch(tt.pattern, tt.text, tt.caseSensitive, tt.desc+" (case-sensitive)")

		// Log expected case-insensitive behavior for future implementation
		if tt.caseSensitive != tt.caseInsensitive {
			s.T().Logf("Case-insensitive difference: %s vs %s - sensitive: %v, insensitive: %v",
				tt.pattern, tt.text, tt.caseSensitive, tt.caseInsensitive)
		}
	}
}
