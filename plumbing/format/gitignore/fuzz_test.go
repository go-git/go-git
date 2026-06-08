package gitignore

import (
	"strings"
	"testing"
)

func FuzzMatch(f *testing.F) {
	seeds := []struct{ pattern, path string }{
		{"foo", "foo"},
		{"!foo", "foo"},
		{"*.go", "main.go"},
		{"**/bar", "foo/bar"},
		{"foo/**/bar", "foo/x/y/bar"},
		{"build/", "build/out"},
		{`foo\*`, "foo*"},
		{"[abc]", "a"},
		{"[!abc]", "d"},
		{"[a-z]", "m"},
		{"[[:alpha:]]", "A"},
		{"[[:digit:]]", "5"},
		{"[[:unknown:]]", "x"},
		{"[", "["},
		{"[unterminated", "x"},
		{`\`, `\`},
		{"", ""},
		{"a/b/c", "a/b/c"},
	}
	for _, s := range seeds {
		f.Add(s.pattern, s.path)
	}

	f.Fuzz(func(_ *testing.T, pattern, path string) {
		p := ParsePattern(pattern, nil)

		isDir := strings.HasSuffix(path, "/")
		segments := strings.Split(strings.Trim(path, "/"), "/")
		if len(segments) == 1 && segments[0] == "" {
			segments = nil
		}

		_ = p.Match(segments, isDir)
	})
}
