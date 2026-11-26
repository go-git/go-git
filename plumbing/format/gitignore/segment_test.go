package gitignore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

func TestMatchSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		glob, name string
		want       bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "ab", false},
		{"abc", "abcd", false},
		{"abc", "ABC", false},
		{"abc", "", false},

		{"*", "", true},
		{"*", "anything", true},
		{"**", "", true},
		{"***", "x", true},
		{"a*", "a", true},
		{"a*", "abc", true},
		{"a*", "ba", false},
		{"*a", "a", true},
		{"*a", "bba", true},
		{"*a", "ab", false},
		{"a*b", "ab", true},
		{"a*b", "axxb", true},
		{"a*b", "axxbx", false},
		{"a*b", "abab", true},
		{"a**b", "ab", true},
		{"a*b*c", "abc", true},
		{"a*b*c", "aXbXbXc", true},
		{"a*b*c", "aXbXbX", false},
		{"*.go", "main.go", true},
		{"*.go", ".go", true},
		{"*.go", "main.go.bak", false},
		{"*a*", "", false},
		{"*ab", "aab", true},
		{"*aab", "aaab", true},
		{"*abab", "ababab", true},
		{"*a?", "aab", true},
		{"*[ab]c", "aac", true},
		{`*\*`, "a*", true},
		{`*\*`, "a", false},

		{"?", "a", true},
		{"?", "", false},
		{"?", "ab", false},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"??", "\xc3\xa9", true},
		{"?", "\xc3\xa9", false},
		{"*?", "", false},
		{"*?", "a", true},
		{"?*?", "a", false},
		{"?*?", "ab", true},

		{"[abc]", "b", true},
		{"[abc]", "d", false},
		{"[abc]", "", false},
		{"[abc]", "ab", false},
		{"x[0-9]y", "x5y", true},
		{"x[0-9]y", "xay", false},
		{"[!a]", "b", true},
		{"[!a]", "a", false},
		{"[a]b]", "ab]", true},
		{"[a]b]", "b", false},
		{"[[:digit:]]*", "1abc", true},
		{"*[[:digit:]]", "abc", false},
		{"[a-c][x-z]", "bz", true},
		{"[a-c][a-c][a-c][a-c][a-c][a-c]", "abcabc", true},
		{"[a-c][a-c][a-c][a-c][a-c][!a-c]", "abcabc", false},

		{`\a`, "a", true},
		{`\*`, "*", true},
		{`\*`, "a", false},
		{`\?`, "?", true},
		{`\?`, "a", false},
		{`\[a]`, "[a]", true},
		{`\[a]`, "a", false},
		{`\\`, `\`, true},
		{`a\\b`, `a\b`, true},

		{`\`, `\`, false},
		{`\`, "", false},
		{`a\`, "a", false},
		{`a\`, `a\`, false},
		{`*\`, `\`, false},
		{"[", "[", false},
		{"[a", "a", false},
		{"[a", "[a", false},
		{"a[", "a[", false},
		{"*[", "x[", false},
		{"[[:bogus:]]", "b", false},
		{"x[[:bogus:]]", "xb", false},
		{"*[[:bogus:]]*", "anything", false},
		{`[a\`, "a", false},
	}

	for _, tc := range tests {
		var brackets bracketCache
		assert.Equal(t, tc.want, matchSegment(tc.glob, tc.name, &brackets), "glob %q, name %q", tc.glob, tc.name)
		assert.Equal(t, tc.want, matchSegment(tc.glob, tc.name, &brackets), "glob %q, name %q, warm cache", tc.glob, tc.name)
	}
}

// referenceMatch is the definition of a segment match written for clarity
// alone: every '*' tries every length.
func referenceMatch(glob, name string) bool {
	if glob == "" {
		return name == ""
	}

	switch glob[0] {
	case '*':
		for skip := 0; skip <= len(name); skip++ {
			if referenceMatch(glob[1:], name[skip:]) {
				return true
			}
		}
		return false
	case '\\':
		return len(glob) > 1 && name != "" && glob[1] == name[0] && referenceMatch(glob[2:], name[1:])
	case '[':
		var set bracketSet
		readBracket(&set, glob, 0)
		return !set.malformed && name != "" && set.has(name[0]) && referenceMatch(glob[set.end+1:], name[1:])
	case '?':
		return name != "" && referenceMatch(glob[1:], name[1:])
	default:
		return name != "" && glob[0] == name[0] && referenceMatch(glob[1:], name[1:])
	}
}

// wellFormed reports whether every escape and bracket expression of glob is
// complete. A malformed glob matches nothing even where a match would not
// need the malformed part, which referenceMatch does not model.
func wellFormed(glob string) bool {
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '\\':
			if i++; i == len(glob) {
				return false
			}
		case '[':
			var set bracketSet
			readBracket(&set, glob, i)
			if set.malformed {
				return false
			}
			i = set.end
		}
	}
	return true
}

// TestMatchSegmentAgainstReference tries every glob and name up to a
// length over a small alphabet, which covers each way '*' can interleave
// with the other pattern elements.
func TestMatchSegmentAgainstReference(t *testing.T) {
	t.Parallel()

	words := func(alphabet string, maxLen int) []string {
		last := []string{""}
		all := slices.Clone(last)
		for range maxLen {
			next := make([]string, 0, len(alphabet)*len(last))
			for _, w := range last {
				for _, c := range alphabet {
					next = append(next, w+string(c))
				}
			}
			all = append(all, next...)
			last = next
		}
		return all
	}

	names := words("ab]", 5)
	for _, glob := range words(`ab*?[]\`, 5) {
		if glob == "" {
			continue
		}
		var brackets bracketCache
		for _, name := range names {
			want := wellFormed(glob) && referenceMatch(glob, name)
			if got := matchSegment(glob, name, &brackets); got != want {
				t.Fatalf("matchSegment(%q, %q) = %v, want %v", glob, name, got, want)
			}
		}
	}
}

// A glob with many '*' must not make matching cost grow exponentially
// with its length: an ignore file is untrusted input.
func TestMatchSegmentManyStars(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("a", 20_000)
	globs := []string{
		strings.Repeat("*a", 200) + "b",
		strings.Repeat("a*", 200) + "b",
		strings.Repeat("*a", 200) + "*b*",
		strings.Repeat("*[a]", 200) + "b",
		strings.Repeat("*?", 200) + "b",
		strings.Repeat("**a", 100) + "[!a]",
	}

	start := time.Now()
	for _, glob := range globs {
		var brackets bracketCache
		assert.False(t, matchSegment(glob, name, &brackets), glob[:8])
	}
	var brackets bracketCache
	assert.True(t, matchSegment(strings.Repeat("*a", 200), name, &brackets))

	// Bounded by len(glob) * len(name) this is a few million steps per
	// glob. Exponential backtracking would not finish.
	assert.Less(t, time.Since(start), 30*time.Second)
}

// TestMatchSegmentMatchesGit puts each glob in a .gitignore of its own and
// compares, for a list of file names, what git check-ignore reports with
// what ParsePattern and Match decide.
func TestMatchSegmentMatchesGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("oracle disabled: the names include bytes a Windows path cannot carry")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("oracle disabled: git not found: %v", err)
	}

	globs := []string{
		"abc", "*", "**", "***", "a*", "*a", "a*b", "a**b", "a*b*c", "*a*", "*ab", "*aab", "*abab",
		"*.go", "*a?", "*[ab]c", `*\*`, "?", "??", "a?c", "*?", "?*?", "?*", "*?*",
		"[abc]", "x[0-9]y", "[!a]", "[a]b]", "[[:digit:]]*", "*[[:digit:]]", "[a-c][x-z]",
		`\a`, `\*`, `\?`, `\[a]`, `\\`, `a\\b`, `a\*`, `*\?*`, `[*]`, `[?]`, `[\*]`, `a[*]b`,
		`\`, `a\`, `*\`, "[", "[a", "a[", "*[", "[[:bogus:]]", "x[[:bogus:]]", "*[[:bogus:]]*", `[a\`,
		"a*[", "*a[b", `ab*\`,
	}
	names := []string{
		"a", "b", "c", "ab", "abc", "abd", "abcd", "ABC", "ba", "bba", "axxb", "axxbx", "abab",
		"aXbXbXc", "aXbXbX", "main.go", ".go", "main.go.bak", "aab", "aaab", "ababab", "aac", "a*",
		"*", "?", "a?", "x5y", "xay", "ab]", "1abc", "bz", "[a]", `\`, `a\b`, `a\`, "[", "[a", "a[",
		"x[", "xb", "a*b", "a?b", "é", "anything", "a[b", "ab",
	}
	var stdin strings.Builder
	for _, name := range names {
		stdin.WriteString("./" + name + "\x00")
	}

	dir := t.TempDir()
	require.NoError(t, gitenv.CommandContext(t.Context(), "git", "-C", dir, "init", "-q").Run())

	// One glob at a time: they share the .gitignore file.
	for _, glob := range globs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(glob+"\n"), 0o600))
		cmd := gitenv.CommandContext(t.Context(), "git", "-C", dir, "-c", "core.ignorecase=false", "check-ignore", "--no-index", "--stdin", "-z")
		cmd.Stdin = strings.NewReader(stdin.String())
		out, err := cmd.Output()
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
			err = nil // no name is ignored
		}
		require.NoError(t, err, glob)

		ignored := map[string]bool{}
		for path := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
			ignored[strings.TrimPrefix(path, "./")] = true
		}

		pattern := ParsePattern(glob, nil)
		for _, name := range names {
			got := pattern.Match([]string{name}, false) == Exclude
			assert.Equal(t, ignored[name], got, "glob %q, name %q", glob, name)
		}
	}
}
