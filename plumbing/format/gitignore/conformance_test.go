package gitignore

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
)

// ConformanceSuite verifies go-git's gitignore against upstream Git references:
//
//   - Wildmatch:        git.git t/t3070-wildmatch.sh
//   - Ignore semantics: git.git t/t0008-ignores.sh
//   - POSIX class ASCII: git.git sane-ctype.h (high-bit bytes never classify)
//
// When `-short` is not set, testMatch additionally cross-validates the
// expected value against the system git binary via `git check-ignore`.
type ConformanceSuite struct {
	suite.Suite

	oracleOnce sync.Once
	oracleRoot *os.Root // nil when oracle disabled
}

func TestConformanceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ConformanceSuite))
}

// gitOracleRoot lazily provisions a scratch repo for `git check-ignore` and
// opens it as an *os.Root so file operations are confined to that directory.
// Returns nil when the oracle is unavailable (-short, missing binary, init failed).
func (s *ConformanceSuite) gitOracleRoot() *os.Root {
	s.oracleOnce.Do(func() {
		if testing.Short() {
			return
		}
		if _, err := exec.LookPath("git"); err != nil {
			s.T().Logf("oracle disabled: git not found: %v", err)
			return
		}
		dir, err := os.MkdirTemp("", "gitignore-oracle-")
		if err != nil {
			s.T().Logf("oracle disabled: tempdir: %v", err)
			return
		}
		if err := exec.Command("git", "-c", "init.defaultBranch=main", "-C", dir, "init", "-q").Run(); err != nil {
			_ = os.RemoveAll(dir)
			s.T().Logf("oracle disabled: git init: %v", err)
			return
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			_ = os.RemoveAll(dir)
			s.T().Logf("oracle disabled: open root: %v", err)
			return
		}
		s.oracleRoot = root
	})
	return s.oracleRoot
}

func (s *ConformanceSuite) TearDownSuite() {
	if s.oracleRoot != nil {
		dir := s.oracleRoot.Name()
		_ = s.oracleRoot.Close()
		_ = os.RemoveAll(dir)
	}
}

// gitOracle queries `git check-ignore` for a path against a list of
// .gitignore patterns. Returns (matched, ok); ok=false means the oracle
// could not run or the input can't be faithfully encoded.
func (s *ConformanceSuite) gitOracle(patterns []string, path string, isDir bool) (bool, bool) {
	root := s.gitOracleRoot()
	if root == nil {
		return false, false
	}
	if path == "" || len(patterns) == 0 {
		return false, false
	}
	for _, p := range patterns {
		if p == "" || strings.ContainsAny(p, "\n\r") {
			return false, false
		}
	}
	// `.` and `..` can't be real filenames; `//` can't appear in a real path.
	// These appear in t3070 to exercise the wildmatch primitive in isolation,
	// not gitignore semantics, so the .gitignore oracle has nothing to say.
	if path == "." || path == ".." || strings.Contains(path, "//") {
		return false, false
	}
	if err := cleanScratch(root); err != nil {
		return false, false
	}
	content := strings.Join(patterns, "\n") + "\n"
	if err := root.WriteFile(".gitignore", []byte(content), 0o644); err != nil {
		return false, false
	}
	if err := materialize(root, path, isDir); err != nil {
		return false, false
	}
	arg := path
	if isDir && !strings.HasSuffix(arg, "/") {
		arg += "/"
	}
	// `check-ignore` (without --no-index) stats the path so it can distinguish
	// dirs from files, which is the only way it produces the same answer as
	// `git status` for re-include patterns over directories.
	cmd := exec.Command("git", "-C", root.Name(), "check-ignore", "-q", "--", arg)
	err := cmd.Run()
	switch {
	case err == nil:
		return true, true // exit 0: ignored
	case exitCode(err) == 1:
		return false, true // exit 1: not ignored
	default:
		return false, false
	}
}

func exitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}

// testMatch checks a single pattern against a single path, mirroring
// t3070-wildmatch.sh's (glob, text) entries. When the oracle is enabled,
// it also asserts the expected value against the system git binary.
func (s *ConformanceSuite) testMatch(pattern, text string, expected bool, desc string) {
	p := ParsePattern(pattern, nil)
	isDir := strings.HasSuffix(text, "/")
	pathSlice := strings.Split(strings.Trim(text, "/"), "/")
	if pathSlice[0] == "" {
		pathSlice = []string{}
	}
	result := p.Match(pathSlice, isDir)
	s.Equal(expected, result == Exclude, "%s: pattern=%q text=%q result=%v", desc, pattern, text, result)

	if gitGot, ok := s.gitOracle([]string{pattern}, text, isDir); ok {
		s.Equal(expected, gitGot,
			"oracle disagrees with expectation: pattern=%q text=%q expected=%v git=%v (%s)",
			pattern, text, expected, gitGot, desc)
	}
}

// createMatcher builds a Matcher from .gitignore-style lines, dropping blanks
// and comments — the parsing performed by t0008-ignores.sh's setup.
func (s *ConformanceSuite) createMatcher(patterns []string) Matcher {
	ps := make([]Pattern, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		ps = append(ps, ParsePattern(pattern, nil))
	}
	return NewMatcher(ps)
}

// assertIgnore matches path against the multi-pattern Matcher m, asserts the
// result equals expected, and (if the oracle is enabled) asserts the same
// expectation against `git check-ignore` over the same patterns list.
func (s *ConformanceSuite) assertIgnore(m Matcher, patterns []string, path string, isDir, expected bool, desc string) {
	pathSlice := strings.Split(strings.Trim(path, "/"), "/")
	if pathSlice[0] == "" {
		pathSlice = []string{}
	}
	got := m.Match(pathSlice, isDir)
	s.Equal(expected, got, "%s: path=%q isDir=%v", desc, path, isDir)

	if gitGot, ok := s.gitOracle(patterns, path, isDir); ok {
		s.Equal(expected, gitGot,
			"oracle disagrees: patterns=%v path=%q isDir=%v expected=%v git=%v (%s)",
			patterns, path, isDir, expected, gitGot, desc)
	}
}

func (s *ConformanceSuite) TestWildmatchBasic() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
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
		// Git's wildmatch enters the escape branch on a trailing `\` and
		// compares t_ch to NUL, returning NOMATCH (cf. wildmatch.c).
		{"foo\\", "foo\\", false, "trailing backslash doesn't match"},
	}
	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *ConformanceSuite) TestWildmatchBrackets() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
		{"*[al]?", "ball", true, "character set match"},
		{"[ten]", "ten", false, "character set literal no match"},
		{"**[!te]", "ten", true, "negated character set [!te]"},
		{"**[!ten]", "ten", false, "negated character set [!ten]"},
		{"t[a-g]n", "ten", true, "character range [a-g]"},
		{"t[!a-g]n", "ten", false, "negated range [!a-g] vs e"},
		{"t[!a-g]n", "ton", true, "negated range [!a-g] vs o"},
		{"t[^a-g]n", "ton", true, "caret negation [^a-g]"},

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

func (s *ConformanceSuite) TestWildmatchSlashSemantics() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
		{"foo*bar", "foo/baz/bar", false, "single star doesn't cross slash in glob mode"},
		{"foo**bar", "foo/baz/bar", false, "double star without slashes"},
		{"foo**bar", "foobazbar", true, "double star within segment"},
		{"foo/**/bar", "foo/baz/bar", true, "double star with slashes"},
		{"foo/**/**/bar", "foo/baz/bar", true, "multiple double star segments"},
		{"foo/**/bar", "foo/b/a/z/bar", true, "double star multiple intermediate"},
		{"foo/**/**/bar", "foo/b/a/z/bar", true, "multiple double star multiple intermediate"},
		{"foo/**/bar", "foo/bar", true, "double star zero intermediate"},
		{"foo/**/**/bar", "foo/bar", true, "multiple double star zero intermediate"},

		{"foo?bar", "foo/bar", false, "question mark doesn't cross slash"},
		{"foo[/]bar", "foo/bar", false, "bracket slash doesn't cross"},
		{"foo[^a-z]bar", "foo/bar", false, "bracket negation doesn't cross slash"},
		{"f[^eiu][^eiu][^eiu][^eiu][^eiu]r", "foo/bar", false, "multiple bracket negations"},
		{"f[^eiu][^eiu][^eiu][^eiu][^eiu]r", "foo-bar", true, "multiple bracket negations with dash"},

		{"**/foo", "foo", true, "double star from root immediate"},
		{"**/foo", "XXX/foo", true, "double star from root nested"},
		{"**/foo", "bar/baz/foo", true, "double star from root deep"},
		{"*/foo", "bar/baz/foo", false, "single star doesn't match deep"},

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

		// Path-relative patterns (t3070's pathmatch entries).
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
		{"*Xg*i", "ab/cXd/efXg/hi", false, "X pattern across slash (glob mode)"},
	}
	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *ConformanceSuite) TestWildmatchCharacterClasses() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
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
		{"[[:xdigit:]]", "g", false, "non-hex letter"},

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

// TestPOSIXClassesAreASCIIOnly verifies that high-bit bytes never satisfy any
// POSIX class. Git classifies via sane-ctype.h, whose tables only populate
// 0x00–0x7F; any byte >= 0x80 falls through as "no class".
func (s *ConformanceSuite) TestPOSIXClassesAreASCIIOnly() {
	upperAGrave := "\xc0"  // 'À' as a single Latin-1 byte
	superscript2 := "\xb2" // '²' as a single Latin-1 byte (Unicode digit)
	nbsp := "\xa0"         // NBSP — Unicode space, not ASCII space

	cases := []struct {
		pattern, text string
		want          bool
		desc          string
	}{
		{`[[:alpha:]]`, upperAGrave, false, "0xC0 not ASCII alpha"},
		{`[[:upper:]]`, upperAGrave, false, "0xC0 not ASCII upper"},
		{`[[:digit:]]`, superscript2, false, "0xB2 not ASCII digit"},
		{`[[:space:]]`, nbsp, false, "0xA0 not ASCII space"},
		{`[[:print:]]`, upperAGrave, false, "0xC0 not ASCII printable"},
		{`[[:punct:]]`, upperAGrave, false, "0xC0 not ASCII punct"},
		{`[[:alpha:]]`, "A", true, "ASCII A matches alpha"},
		{`[[:digit:]]`, "5", true, "ASCII 5 matches digit"},
		{`[[:space:]]`, " ", true, "ASCII space matches space"},
	}
	for _, c := range cases {
		s.testMatch(c.pattern, c.text, c.want, c.desc)
	}
}

func (s *ConformanceSuite) TestWildmatchMalformedBrackets() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
		{"[\\-^]", "]", false, "backslash dash caret"},
		{"[\\-^]", "[", false, "backslash dash caret vs opening bracket"},
		{"[\\-^]", "-", true, "dash matches escaped dash range"},
		{"[\\-_]", "-", true, "backslash dash underscore vs dash"},
		{"[\\]]", "]", true, "escaped closing bracket"},
		{"[\\]]", "\\]", false, "escaped bracket vs literal"},
		{"[\\]]", "\\", false, "escaped bracket vs backslash"},

		{"a[]b", "ab", false, "empty bracket set"},
		{"a[]b", "a[]b", false, "empty brackets are invalid"},
		{"ab[", "ab[", false, "unclosed bracket is invalid"},
		{"[!", "ab", false, "incomplete negation"},
		{"[-", "ab", false, "incomplete dash"},
		{"[-]", "-", true, "lone dash in brackets"},
		{"[a-", "-", false, "incomplete range"},
		{"[!a-", "-", false, "incomplete negated range"},

		{"[--A]", "-", true, "dash to letter range includes dash"},
		{"[--A]", "5", true, "dash to letter range includes number"},
		{"[ --]", " ", true, "space to dash range includes space"},
		{"[ --]", "$", true, "space to dash range includes dollar"},
		{"[ --]", "-", true, "space to dash range includes dash"},
		{"[ --]", "0", false, "space to dash range excludes zero"},
		{"[---]", "-", true, "triple dash"},
		{"[------]", "-", true, "many dashes"},

		{"[a-e-n]", "j", false, "invalid double range vs j"},
		{"[a-e-n]", "-", true, "invalid double range vs dash"},
		{"[!------]", "a", true, "negated many dashes vs letter"},

		{"[]-a]", "[", false, "bracket dash a vs opening bracket"},
		{"[]-a]", "^", true, "bracket dash a vs caret"},
		{"[!]-a]", "^", false, "negated bracket dash a vs caret"},
		{"[!]-a]", "[", true, "negated bracket dash a vs bracket"},
		{"[a^bc]", "^", true, "caret in character set"},
		{"[a-]b]", "-b]", true, "dash bracket literal"},

		{"[\\]", "\\", false, "single backslash in brackets"},
		{"[\\\\]", "\\", true, "escaped backslash in brackets"},
		{"[!\\\\]", "\\", false, "negated backslash"},
		{"[A-\\\\]", "G", true, "range to backslash"},

		{"b*a", "aaabbb", false, "suffix pattern no match"},
		{"*ba*", "aabcaa", false, "middle pattern no match"},
		{"[,]", ",", true, "comma in brackets"},
		{"[\\\\,]", ",", true, "comma and backslash in brackets"},
		{"[\\\\,]", "\\", true, "backslash in comma brackets"},
		{"[,-.]", "-", true, "comma to period range includes dash"},
		{"[,-.]", "+", false, "comma to period excludes plus"},
		{"[,-.]", "-.]", false, "comma period range vs literal"},

		{"[\\1-\\3]", "2", true, "octal range includes 2"},
		{"[\\1-\\3]", "3", true, "octal range includes 3"},
		{"[\\1-\\3]", "4", false, "octal range excludes 4"},

		{"[[-\\]]", "\\", true, "bracket to bracket range includes backslash"},
		{"[[-\\]]", "[", true, "bracket to bracket range includes opening bracket"},
		{"[[-\\]]", "]", true, "bracket to bracket range includes closing bracket"},
		{"[[-\\]]", "-", false, "bracket to bracket range excludes dash"},
	}
	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *ConformanceSuite) TestWildmatchRecursion() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
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

		{"*/*/*", "foo", false, "three level pattern vs one level"},
		{"*/*/*", "foo/bar", false, "three level pattern vs two levels"},
		{"*/*/*", "foo/bb/aa", true, "three level pattern exact match"},
		{"*/*/*", "foo/bba/arr", true, "three level pattern across longer names"},
		{"*/*/*", "foo/bb/aa/rr", true, "three level pattern matches deeper"},
		{"**/**/**", "foo/bb/aa/rr", true, "triple double star"},

		{"*X*i", "abcXdefXghi", true, "X marker pattern"},
		{"*X*i", "ab/cXd/efXg/hi", false, "X marker across slashes (glob mode)"},
		{"*/*X*/*/*i", "ab/cXd/efXg/hi", true, "structured X pattern"},
		{"**/*X*/**/*i", "ab/cXd/efXg/hi", true, "double star X pattern"},
	}
	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

func (s *ConformanceSuite) TestWildmatchVarious() {
	tests := []struct {
		pattern, text string
		expected      bool
		desc          string
	}{
		{"a[c-c]st", "acrt", false, "single character range no match"},
		{"a[c-c]rt", "acrt", true, "single character range match"},
		{"[!]-]", "]", false, "negated bracket with closing bracket"},
		{"[!]-]", "a", true, "negated bracket with letter"},

		{"\\", "", false, "single backslash vs empty"},
		// Trailing-backslash patterns: Git's wildmatch returns NOMATCH because
		// the escape branch advances past `\` into NUL.
		{"\\", "\\", false, "lone backslash doesn't match itself"},
		{"*/\\", "XXX/\\", false, "trailing backslash after star doesn't match"},
		{"*/\\\\", "XXX/\\", true, "escaped backslash path pattern"},

		{"@foo", "@foo", true, "at-sign prefix exact"},
		{"@foo", "foo", false, "at-sign prefix no match"},

		{"\\[ab]", "[ab]", true, "escaped opening bracket"},
		{"[[]ab]", "[ab]", true, "bracket in character set"},
		{"[[:]ab]", "[ab]", true, "bracket colon in set"},
		{"[[::]ab]", "[ab]", false, "double colon in set"},
		{"[[:digit]ab]", "[ab]", true, "partial digit class"},
		{"[\\[:]ab]", "[ab]", true, "escaped bracket in set"},

		{"\\??\\?b", "?a?b", true, "escaped question marks"},
		{"\\a\\b\\c", "abc", true, "escaped letters"},

		{"", "foo", false, "empty pattern vs text"},
		{"**/t[o]", "foo/bar/baz/to", true, "double star with bracket"},
	}
	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.expected, tt.desc)
	}
}

// TestWildmatchCaseSensitivity asserts go-git's current case-sensitive
// behavior. Git also has case-insensitive (iglob) entries in t3070; those are
// not exercised because gitignore matching is case-sensitive by default.
func (s *ConformanceSuite) TestWildmatchCaseSensitivity() {
	tests := []struct {
		pattern, text string
		caseSensitive bool
		desc          string
	}{
		{"[A-Z]", "a", false, "uppercase range vs lowercase"},
		{"[A-Z]", "A", true, "uppercase range vs uppercase"},
		{"[a-z]", "A", false, "lowercase range vs uppercase"},
		{"[a-z]", "a", true, "lowercase range vs lowercase"},
		{"[[:upper:]]", "a", false, "upper class vs lowercase"},
		{"[[:upper:]]", "A", true, "upper class vs uppercase"},
		{"[[:lower:]]", "A", false, "lower class vs uppercase"},
		{"[[:lower:]]", "a", true, "lower class vs lowercase"},
		{"[B-Za]", "A", false, "mixed case range"},
		{"[B-a]", "A", false, "reverse case range"},
		{"[Z-y]", "z", false, "reverse case range lowercase"},
		{"[Z-y]", "Z", true, "reverse case range uppercase"},
	}
	for _, tt := range tests {
		s.testMatch(tt.pattern, tt.text, tt.caseSensitive, tt.desc+" (case-sensitive)")
	}
}

// TestIgnoreBasic exercises t0008's top-level .gitignore:
//
//	one
//	ignored-*
//	top-level-dir/
func (s *ConformanceSuite) TestIgnoreBasic() {
	patterns := []string{"one", "ignored-*", "top-level-dir/"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		isDir   bool
		ignored bool
		desc    string
	}{
		{"one", false, true, "simple pattern match"},
		{"one", true, true, "simple pattern match for directory"},
		{"ignored-and-untracked", false, true, "wildcard pattern"},
		{"ignored-but-in-index", false, true, "wildcard pattern variant"},
		{"not-ignored", false, false, "non-matching file"},
		{"not-ignored", true, false, "non-matching directory"},
		{"top-level-dir", true, true, "directory-only pattern matches dir"},
		{"top-level-dir", false, false, "directory-only pattern doesn't match file"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, tt.isDir, tt.ignored, tt.desc)
	}
}

// TestIgnoreNested exercises t0008's a/.gitignore:
//
//	two*
//	*three
func (s *ConformanceSuite) TestIgnoreNested() {
	patterns := []string{"two*", "*three"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"3-three", true, "suffix wildcard match"},
		{"three-not-this-one", false, "suffix wildcard non-match"},
		{"twoooo", true, "prefix wildcard match"},
		{"not-two", false, "prefix wildcard non-match"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, false, tt.ignored, tt.desc)
	}
}

// TestIgnoreNegation exercises t0008's a/b/.gitignore (subset):
//
//	four
//	five
//	six
//	ignored-dir/
//	!on*
//	!two
func (s *ConformanceSuite) TestIgnoreNegation() {
	patterns := []string{"four", "five", "six", "ignored-dir/", "!on*", "!two"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		isDir   bool
		ignored bool
		desc    string
	}{
		{"four", false, true, "basic ignore"},
		{"five", false, true, "basic ignore"},
		{"six", false, true, "basic ignore"},
		{"one", false, false, "negated by !on*"},
		{"only", false, false, "negated by !on*"},
		{"on-something", false, false, "negated by !on*"},
		{"two", false, false, "explicitly negated"},
		{"ignored-dir", true, true, "directory pattern"},
		{"ignored-dir", false, false, "directory pattern for file"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, tt.isDir, tt.ignored, tt.desc)
	}
}

// TestIgnoreExactPrefix verifies t0008's behavior that /git/ and git/ both
// anchor on the directory name, not on substring prefixes.
func (s *ConformanceSuite) TestIgnoreExactPrefix() {
	tests := []struct {
		name     string
		patterns []string
	}{
		{"with root", []string{"/git/"}},
		{"without root", []string{"git/"}},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			m := s.createMatcher(test.patterns)

			testCases := []struct {
				path    string
				isDir   bool
				ignored bool
			}{
				{"git", true, true},
				{"git/foo", false, true},
				{"git-foo", true, false},
				{"git-foo/bar", false, false},
			}
			for _, tc := range testCases {
				s.assertIgnore(m, test.patterns, tc.path, tc.isDir, tc.ignored, test.name)
			}
		})
	}
}

// TestIgnoreDoubleStarReinclude exercises t0008's data/** layered with
// re-includes for directories and *.txt.
func (s *ConformanceSuite) TestIgnoreDoubleStarReinclude() {
	patterns := []string{"data/**", "!data/**/", "!data/**/*.txt"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		isDir   bool
		ignored bool
		desc    string
	}{
		{"data/file", false, true, "file under data/ ignored by data/**"},
		{"data/data1/file1", false, true, "nested file ignored"},
		{"data/data1/file1.txt", false, false, "txt file re-included"},
		{"data/data2/file2", false, true, "another nested file ignored"},
		{"data/data2/file2.txt", false, false, "another txt file re-included"},
		{"data", true, false, "data directory itself not ignored"},
		{"data/data1", true, false, "subdirectories re-included by !data/**/"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, tt.isDir, tt.ignored, tt.desc)
	}
}

// TestIgnoreDoubleStarPrefix verifies that foo**/bar matches foo/bar (and
// foo<anything>/bar) but does not match foobar — i.e. ** does not collapse
// across the slash boundary into a prefix.
func (s *ConformanceSuite) TestIgnoreDoubleStarPrefix() {
	patterns := []string{"foo**/bar"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"foo/bar", true, "should match foo/bar"},
		{"foobar", false, "should not match foobar"},
		{"foo123/bar", true, "should match foo123/bar"},
		{"foobar/baz", false, "should not match foobar/baz"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, false, tt.ignored, tt.desc)
	}
}

// TestIgnoreTrailingWhitespace verifies t0008's rule that unescaped trailing
// whitespace in a pattern is stripped.
func (s *ConformanceSuite) TestIgnoreTrailingWhitespace() {
	patterns := []string{"trailing   ", "normal"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"trailing", true, "trailing whitespace in pattern ignored"},
		{"normal", true, "normal pattern works"},
		{"trailing   ", false, "literal trailing spaces don't match"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, false, tt.ignored, tt.desc)
	}
}

// TestIgnorePrecedence verifies that the last matching pattern wins, including
// when negation and re-exclusion alternate.
func (s *ConformanceSuite) TestIgnorePrecedence() {
	patterns := []string{"*.txt", "!important.txt", "*.log", "!debug.log"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"file.txt", true, "txt files ignored"},
		{"important.txt", false, "important.txt re-included"},
		{"file.log", true, "log files ignored"},
		{"debug.log", false, "debug.log re-included"},
		{"other.doc", false, "unmatched files not ignored"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, false, tt.ignored, tt.desc)
	}
}

// TestIgnoreRealWorld smoke-tests a real .gitignore composed of common
// patterns to catch interactions between rules.
func (s *ConformanceSuite) TestIgnoreRealWorld() {
	patterns := []string{
		"node_modules/",
		"*.log",
		"dist/",
		"build/",
		".env*",
		"!.env.example",
		"coverage/",
		"*.tmp",
		".DS_Store",
		"thumbs.db",
		"*.swp",
		"*.swo",
		"*~",
	}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		isDir   bool
		ignored bool
		desc    string
	}{
		{"node_modules", true, true, "node_modules directory"},
		{"node_modules/react/index.js", false, true, "file in node_modules"},
		{"app.log", false, true, "log file"},
		{"logs/debug.log", false, true, "log file in subdirectory"},
		{"dist", true, true, "dist directory"},
		{"build", true, true, "build directory"},
		{".env", false, true, "env file"},
		{".env.local", false, true, "env local file"},
		{".env.example", false, false, "env example re-included"},
		{"coverage", true, true, "coverage directory"},
		{"temp.tmp", false, true, "tmp file"},
		{".DS_Store", false, true, "mac system file"},
		{"file.swp", false, true, "vim swap file"},
		{"backup~", false, true, "backup file"},
		{"src/main.js", false, false, "normal source file"},
		{"README.md", false, false, "normal doc file"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, tt.isDir, tt.ignored, tt.desc)
	}
}

// TestIgnoreBlanksAndComments verifies t0008's parsing rules: blank lines and
// '#'-prefixed lines are dropped, and a bare ' ' is treated as blank.
func (s *ConformanceSuite) TestIgnoreBlanksAndComments() {
	patterns := []string{"", "# comment", "normal", " ", "!"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"normal", true, "normal pattern works"},
		{"", false, "empty path not ignored"},
		{"comment", false, "comment-like file not ignored"},
		{" ", false, "space file not ignored"},
		{"!", false, "exclamation file not ignored"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, false, tt.ignored, tt.desc)
	}
}

// TestIgnoreNegatedBracket verifies bracket negation in matcher context.
// Cross-checked against Git: echo '[!a]bc' > .gitignore && git check-ignore -v
// abc xbc !bc
func (s *ConformanceSuite) TestIgnoreNegatedBracket() {
	patterns := []string{"[!a]bc", "[0-9]*.txt"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"!bc", true, "[!a]bc matches !bc (! is not 'a', so [!a] matches)"},
		{"abc", false, "[!a]bc doesn't match abc (a matches [!a] negation)"},
		{"xbc", true, "[!a]bc matches xbc (x doesn't match a, so [!a] matches)"},
		{"bbc", true, "[!a]bc matches bbc"},
		{"1test.txt", true, "[0-9]*.txt matches 1test.txt"},
		{"5data.txt", true, "[0-9]*.txt matches 5data.txt"},
		{"atest.txt", false, "atest.txt doesn't match [0-9]*"},
	}
	for _, tt := range tests {
		s.assertIgnore(m, patterns, tt.path, false, tt.ignored, tt.desc)
	}
}

// cleanScratch removes everything inside root except `.git` and `.gitignore`
// so each oracle call sees only the materialized path under test. All
// operations are confined to root: a malicious or malformed `name` referring
// outside the directory is rejected by *os.Root.
func cleanScratch(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" || e.Name() == ".gitignore" {
			continue
		}
		if err := root.RemoveAll(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// materialize creates path under root so check-ignore can distinguish
// directory paths from file paths via stat. *os.Root rejects names that
// escape the root (e.g. `..`, absolute paths, symlinks pointing outside),
// so a path argument from the test data cannot create files outside the
// scratch directory.
func materialize(root *os.Root, p string, isDir bool) error {
	if isDir {
		return root.MkdirAll(p, 0o755)
	}
	if i := strings.LastIndex(p, "/"); i > 0 {
		if err := root.MkdirAll(p[:i], 0o755); err != nil {
			return err
		}
	}
	f, err := root.Create(p)
	if err != nil {
		return err
	}
	return f.Close()
}
