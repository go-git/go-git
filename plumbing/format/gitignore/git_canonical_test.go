package gitignore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// GitCanonicalSuite contains tests based on git's official test suite (t0008-ignores.sh)
// These tests verify that go-git's gitignore implementation matches git's reference behavior
type GitCanonicalSuite struct {
	suite.Suite
}

func TestGitCanonicalSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(GitCanonicalSuite))
}

// Helper to create matcher from patterns like git's test setup
func (s *GitCanonicalSuite) createMatcher(patterns []string) Matcher {
	ps := make([]Pattern, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		ps = append(ps, ParsePattern(pattern, nil))
	}
	return NewMatcher(ps)
}

// Helper to test path matching - converts paths to go-git format
func (s *GitCanonicalSuite) testPath(m Matcher, path string, isDir bool) bool {
	pathSlice := strings.Split(strings.Trim(path, "/"), "/")
	if pathSlice[0] == "" {
		pathSlice = []string{}
	}
	return m.Match(pathSlice, isDir)
}

// Test basic ignore patterns from git's setup
func (s *GitCanonicalSuite) TestBasicIgnorePatterns() {
	// From git's test setup:
	// .gitignore:
	//   one
	//   ignored-*
	//   top-level-dir/
	patterns := []string{"one", "ignored-*", "top-level-dir/"}
	m := s.createMatcher(patterns)

	// Test cases from git's official test suite
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
		result := s.testPath(m, tt.path, tt.isDir)
		s.Equal(tt.ignored, result, "%s: %s (isDir: %v)", tt.desc, tt.path, tt.isDir)
	}
}

// Test nested .gitignore behavior
func (s *GitCanonicalSuite) TestNestedGitignore() {
	// From git test setup:
	// a/.gitignore: two*
	//               *three
	// a/b/.gitignore: four
	//                 five
	//                 six
	//                 ignored-dir/
	//                 !on*
	//                 !two

	// Test subdirectory patterns
	subDirPatterns := []string{"two*", "*three"}
	m := s.createMatcher(subDirPatterns)

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
		result := s.testPath(m, tt.path, false)
		s.Equal(tt.ignored, result, "%s: %s", tt.desc, tt.path)
	}
}

// Test negation patterns (!pattern)
func (s *GitCanonicalSuite) TestNegationPatterns() {
	// From git test: a/b/.gitignore has !on* and !two
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
		result := s.testPath(m, tt.path, tt.isDir)
		s.Equal(tt.ignored, result, "%s: %s (isDir: %v)", tt.desc, tt.path, tt.isDir)
	}
}

// Test exact prefix matching from git's test
func (s *GitCanonicalSuite) TestExactPrefixMatching() {
	// Test both /git/ and git/ patterns
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
				{"git", true, true},           // git/ directory should match
				{"git/foo", false, true},      // files under git/ should match
				{"git-foo", true, false},      // git-foo/ should not match
				{"git-foo/bar", false, false}, // files under git-foo/ should not match
			}

			for _, tc := range testCases {
				result := s.testPath(m, tc.path, tc.isDir)
				s.Equal(tc.ignored, result, "path: %s (isDir: %v)", tc.path, tc.isDir)
			}
		})
	}
}

// Test double star patterns from git's test
func (s *GitCanonicalSuite) TestDoubleStarPatterns() {
	// From git test: data/**, !data/**/, !data/**/*.txt
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
		result := s.testPath(m, tt.path, tt.isDir)
		s.Equal(tt.ignored, result, "%s: %s (isDir: %v)", tt.desc, tt.path, tt.isDir)
	}
}

// Test ** not confused by leading prefix
func (s *GitCanonicalSuite) TestDoubleStarNotConfused() {
	// From git test: foo**/bar should match foo/bar but not foobar
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
		result := s.testPath(m, tt.path, false)
		s.Equal(tt.ignored, result, "%s: %s", tt.desc, tt.path)
	}
}

// Test whitespace handling from git's test
func (s *GitCanonicalSuite) TestWhitespaceHandling() {
	// Git ignores trailing whitespace
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
		result := s.testPath(m, tt.path, false)
		s.Equal(tt.ignored, result, "%s: %s", tt.desc, tt.path)
	}
}

// Test pattern precedence (last matching pattern wins)
func (s *GitCanonicalSuite) TestPatternPrecedence() {
	// Test that later patterns override earlier ones
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
		result := s.testPath(m, tt.path, false)
		s.Equal(tt.ignored, result, "%s: %s", tt.desc, tt.path)
	}
}

// Test complex real-world patterns
func (s *GitCanonicalSuite) TestComplexRealWorldPatterns() {
	patterns := []string{
		// Common patterns from real repositories
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
		result := s.testPath(m, tt.path, tt.isDir)
		s.Equal(tt.ignored, result, "%s: %s (isDir: %v)", tt.desc, tt.path, tt.isDir)
	}
}

// Test edge cases and corner scenarios
func (s *GitCanonicalSuite) TestEdgeCases() {
	patterns := []string{"", "# comment", "normal", " ", "!"}
	m := s.createMatcher(patterns)

	// Only "normal" should be a valid pattern
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
		result := s.testPath(m, tt.path, false)
		s.Equal(tt.ignored, result, "%s: %s", tt.desc, tt.path)
	}
}

// Test bracket expressions (this will show the discrepancy with git)
func (s *GitCanonicalSuite) TestBracketExpressions() {
	// git-pkgs/gitignore correctly treats [!a] as negation (not a), matching Git
	// Verified with: echo '[!a]bc' > .gitignore && git check-ignore -v abc xbc !bc
	patterns := []string{"[!a]bc", "[0-9]*.txt"}
	m := s.createMatcher(patterns)

	tests := []struct {
		path    string
		ignored bool
		desc    string
	}{
		{"!bc", true, "[!a]bc matches !bc (! is not 'a', so [!a] matches) ✓"},
		{"abc", false, "[!a]bc doesn't match abc (a matches [!a] negation) ✓"},
		{"xbc", true, "[!a]bc matches xbc (x doesn't match a, so [!a] matches) ✓"},
		{"bbc", true, "[!a]bc matches bbc ✓"},
		{"1test.txt", true, "[0-9]*.txt matches 1test.txt ✓"},
		{"5data.txt", true, "[0-9]*.txt matches 5data.txt ✓"},
		{"atest.txt", false, "atest.txt doesn't match [0-9]* ✓"},
	}

	for _, tt := range tests {
		result := s.testPath(m, tt.path, false)
		s.Equal(tt.ignored, result, "%s: %s", tt.desc, tt.path)
	}
}
