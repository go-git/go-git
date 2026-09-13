package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Cases taken from git's t3070-wildmatch.sh, using the WM_PATHNAME
// ("wildmatch") expectations rather than the "pathmatch" ones.
func TestWildmatchPathname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		// Basics.
		{"foo", "foo", true},
		{"bar", "foo", false},
		{"", "", true},
		{"???", "foo", true},
		{"??", "foo", false},
		{"*", "foo", true},
		{"f*", "foo", true},
		{"*f", "foo", false},
		{"*foo*", "foo", true},
		{"*ob*a*r*", "foobar", true},
		{"*ab", "aaaaaaabababab", true},

		// Escaping.
		{`foo\*`, "foo*", true},
		{`foo\*bar`, "foobar", false},
		{`f\\oo`, `f\oo`, true},

		// Slashes are never matched by '*' or '?'.
		{"foo?bar", "foo/bar", false},
		{"foo*bar", "foo/bar", false},
		{"foo[/]bar", "foo/bar", false},
		{"foo/*", "foo/bar", true},
		{"foo/*", "foo/bba/arr", false},

		// '**' crosses slashes when it is a whole component.
		{"foo/**", "foo/bba/arr", true},
		{"foo/**/bar", "foo/bar", true},
		{"foo/**/bar", "foo/baz/bar", true},
		{"foo/**/bar", "foo/b/a/z/bar", true},
		{"foo/**/bar", "foo/bba/arr", false},
		{"foo/**/", "foo/bar", false},
		{"foo/**/*", "foo/bba/arr", true},
		{"foo/**/arr", "foo/bba/arr", true},
		{"foo/*/*", "foo/bba/arr", true},
		{"**/foo", "foo", true},
		{"**/foo", "a/foo", true},
		{"**/foo", "a/b/foo", true},

		// A non-component "**" degrades to a single '*'.
		{"a**b", "ab", true},
		{"a**b", "a/b", false},

		// Bracket expressions.
		{"a[b]c", "abc", true},
		{"a[!b]c", "abc", false},
		{"a[!b]c", "adc", true},
		{"a[^b]c", "adc", true},
		{"a[b-d]c", "acc", true},
		{"a[b-d]c", "aec", false},
		{"[[:digit:]]", "5", true},
		{"[[:digit:]]", "a", false},
		{"[[:alpha:]][[:digit:]]", "a1", true},
		{"[[:digit:][:alpha:]]", "a", true},
		{"]", "]", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s~%s", tt.pattern, tt.text), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, wildmatch(tt.pattern, tt.text, false))
		})
	}
}

func TestWildmatchCaseFold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		text    string
		icase   bool
		want    bool
	}{
		{"FOO", "foo", false, false},
		{"FOO", "foo", true, true},
		{"foo", "FOO", true, true},
		{"**/FOO/**", "a/foo/b", true, true},
		{"**/FOO/**", "a/foo/b", false, false},
		{"[A-Z]", "a", true, true},
		{"[A-Z]", "a", false, false},
		{"[a-z]", "A", true, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s~%s~%v", tt.pattern, tt.text, tt.icase), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, wildmatch(tt.pattern, tt.text, tt.icase))
		})
	}
}
