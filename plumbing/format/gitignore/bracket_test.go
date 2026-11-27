package gitignore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

// acceptedBytes lists what a bracket expression accepts among the bytes a
// file name can be made of on its own.
func acceptedBytes(b *bracketSet) string {
	var sb strings.Builder
	for c := 1; c < 256; c++ {
		if c != '/' && c != '.' && b.has(byte(c)) {
			sb.WriteByte(byte(c))
		}
	}
	return sb.String()
}

func TestReadBracket(t *testing.T) {
	t.Parallel()

	const (
		digits = "0123456789"
		upper  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		lower  = "abcdefghijklmnopqrstuvwxyz"
	)

	tests := []struct {
		pattern string
		start   int
		end     int
		want    string
	}{
		{"[abc]", 0, 4, "abc"},
		{"x[abc]y", 1, 5, "abc"},
		{"[a-e]", 0, 4, "abcde"},
		{"[a]b]", 0, 2, "a"},
		{"[]a]", 0, 3, "]a"},
		{"[]]", 0, 2, "]"},
		{"[a-]", 0, 3, "-a"},
		{"[-a]", 0, 3, "-a"},
		{`[a\-z]`, 0, 5, "-az"},
		{`[\]]`, 0, 3, "]"},
		{`[\\]`, 0, 3, `\`},
		{`[a-\c]`, 0, 5, "abc"},
		{`[\a-c]`, 0, 5, "abc"},
		{`[+-\]]`, 0, 5, `+,-` + digits + `:;<=>?@` + upper + `[\]`},
		{"[z-a]", 0, 4, "z"},
		{`[a-\\]`, 0, 5, "a"},
		{"[a-c-e]", 0, 6, "-abce"},
		{"[a-c-e-g]", 0, 8, "-abcefg"},
		{"[a-b-]", 0, 5, "-ab"},
		{"[--9]", 0, 4, "-" + digits},
		{"[a-c--9]", 0, 7, "-" + digits + "abc"},
		{"[%--]", 0, 4, "%&'()*+,-"},
		{"[]-a]", 0, 4, "]^_`a"},
		{"[[:digit:]]", 0, 10, digits},
		{"[[:digit:]-z]", 0, 12, "-" + digits + "z"},
		{"[[:digit:]--9a]", 0, 14, "-" + digits + "a"},
		{"[[:upper:][:digit:]]", 0, 19, digits + upper},
		{"[_[:lower:]]", 0, 11, "_" + lower},
		{"[[:blank:]]", 0, 10, "\t "},
		{"[[:space:]]", 0, 10, "\t\n\r "},
		{"[[:xdigit:]]", 0, 11, digits + "ABCDEFabcdef"},
		{"[[a]", 0, 3, "[a"},
		{"[a[]", 0, 3, "[a"},
		{"[[]", 0, 2, "["},
		{"[[]]", 0, 2, "["},
		{"[[-a]", 0, 4, "[\\]^_`a"},
		{"[a-[]", 0, 4, "a"},
		{"[a-[:digit:]]", 0, 11, ":adgit"},
		{"[[:]", 0, 3, ":["},
		{"[[:alpha]", 0, 8, ":[ahlp"},
		{"[[x:]]", 0, 4, ":[x"},
		{"[[=a=]]", 0, 5, "=[a"},
		{`[\[:alpha:]]`, 0, 10, ":[ahlp"},
		{"[a^]", 0, 3, "^a"},
		{"[a!]", 0, 3, "!a"},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			t.Parallel()

			var b bracketSet
			readBracket(&b, tc.pattern, tc.start)
			require.False(t, b.malformed)
			assert.Equal(t, tc.end, b.end)
			assert.Equal(t, tc.want, acceptedBytes(&b))
		})
	}
}

func TestReadBracketNegation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern  string
		end      int
		rejected string
	}{
		{"[!abc]", 5, "abc"},
		{"[^abc]", 5, "abc"},
		{"[!]a]", 4, "]a"},
		{"[^]a]", 4, "]a"},
		{"[!-]", 3, "-"},
		{"[^-a]", 4, "-a"},
		{"[!!]", 3, "!"},
		{"[!^]", 3, "^"},
		{"[^^]", 3, "^"},
		{"[^!]", 3, "!"},
		{"[!--9]", 5, "-./0123456789"},
		{"[![:digit:]]", 11, "0123456789"},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			t.Parallel()

			var b bracketSet
			readBracket(&b, tc.pattern, 0)
			require.False(t, b.malformed)
			assert.Equal(t, tc.end, b.end)
			for c := range 256 {
				rejected := strings.IndexByte(tc.rejected, byte(c)) >= 0
				assert.Equal(t, !rejected, b.has(byte(c)), "byte %#x", c)
			}
		})
	}
}

func TestReadBracketMalformed(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{
		"[", "[]", "[!", "[^", "[!]", "[^]", "[a", "[!a", "[a-", "[a-c", "[abc",
		`[\`, `[a\`, `[a\]`, `[a-\`, `[a-c\]`,
		"[[:alpha:]", "[[:alpha:", "[[:alpha:]-",
		"[[:bogus:]]", "[[:ALPHA:]]", "[[::]]", "[[:al:]pha:]]", "[[: alpha:]]",
	} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			var b bracketSet
			readBracket(&b, pattern, 0)
			assert.True(t, b.malformed)
			for c := range 256 {
				assert.False(t, b.has(byte(c)), "byte %#x", c)
			}
		})
	}
}

func TestPOSIXClassesAreASCIIOnly(t *testing.T) {
	t.Parallel()

	for name := range posixClasses {
		var b bracketSet
		readBracket(&b, "[[:"+name+":]]", 0)
		require.False(t, b.malformed, name)
		for c := 0x80; c < 0x100; c++ {
			assert.False(t, b.has(byte(c)), "[:%s:] holds byte %#x", name, c)
		}
	}
}

// TestReadBracketMatchesGit uses each bracket expression as an ignore
// pattern of its own and asks git check-ignore which one-byte file names
// it ignores. That has to be exactly the set readBracket builds, or
// nothing when the expression is malformed or is not the whole pattern.
func TestReadBracketMatchesGit(t *testing.T) {
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

	fixed := []string{
		"[abc]", "[a-e]", "[]a]", "[!]a]", "[^]a]", "[]]", "[a-]", "[-a]", `[a\-z]`, `[\]]`, `[\\]`,
		`[a-\c]`, `[\a-c]`, `[+-\]]`, "[z-a]", `[a-\\]`, "[a-c-e]", "[a-c-e-g]", "[a-b-]",
		"[--9]", "[!--9]", "[a-c--9]", "[%--]", "[+--]", "[]-a]", `[\--9]`,
		"[[:digit:]-z]", "[[:alpha:]-c]", "[[:alpha:]--9]", "[[:alpha:][:digit:]]", "[[:digit:][-a]",
		"[[a]", "[a[]", "[[]", "[[]]", "[[-a]", `[\[-a]`, "[a-[]", "[a-[:digit:]]",
		"[[:]", "[[:alpha]", "[[x:]]", "[[.a.]]", "[[=a=]]", `[\[:alpha:]]`,
		"[a^]", "[a!]", "[!!]", "[!^]", "[^^]", "[^!]", "[!-]", "[^-a]", "[![:digit:]]",
		"[a]b]",
		"[", "[]", "[!", "[!]", "[^]", "[a", "[!a", "[a-", "[a-c", `[a\]`, `[a-c\]`,
		"[[:alpha:]", "[[:alpha:", "[[:bogus:]]", "[[:ALPHA:]]", "[[::]]", "[[:al:]pha:]]", "[[:x]y:]]",
	}
	patterns := make([]string, 0, len(fixed)+2*len(posixClasses))
	patterns = append(patterns, fixed...)
	for name := range posixClasses {
		patterns = append(patterns, "[[:"+name+":]]", "[![:"+name+":]]")
	}

	// "./" keeps git from reading a leading ":" as pathspec magic.
	var names strings.Builder
	for c := 1; c < 256; c++ {
		if c != '/' && c != '.' {
			names.WriteString("./" + string([]byte{byte(c)}) + "\x00")
		}
	}

	dir := t.TempDir()
	require.NoError(t, gitenv.CommandContext(t.Context(), "git", "-C", dir, "init", "-q").Run())

	// One pattern at a time: they share the .gitignore file.
	for _, pattern := range patterns {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(pattern+"\n"), 0o600))
		cmd := gitenv.CommandContext(t.Context(), "git", "-C", dir, "-c", "core.ignorecase=false", "check-ignore", "--no-index", "--stdin", "-z")
		cmd.Stdin = strings.NewReader(names.String())
		out, err := cmd.Output()
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
			err = nil // no name is ignored
		}
		require.NoError(t, err, pattern)
		ignored := strings.ReplaceAll(strings.ReplaceAll(string(out), "./", ""), "\x00", "")

		var b bracketSet
		readBracket(&b, pattern, 0)
		want := ""
		if !b.malformed && b.end == len(pattern)-1 {
			want = acceptedBytes(&b)
		}
		assert.Equal(t, ignored, want, "pattern %s", pattern)
	}
}

// BenchmarkReadBracketManyClassOpeners reads a bracket expression made of
// "[:" pairs that open no class. Each one asks where the next ']' is, and
// the answer must not be searched for from scratch every time: doubling the
// expression should double the cost, not quadruple it.
func BenchmarkReadBracketManyClassOpeners(b *testing.B) {
	for _, pairs := range []int{1000, 10000, 100000} {
		closed := "[" + strings.Repeat("[:", pairs) + "x]"
		b.Run(fmt.Sprintf("closed/%d", pairs), func(b *testing.B) {
			var set bracketSet
			for b.Loop() {
				readBracket(&set, closed, 0)
			}
		})
		b.Run(fmt.Sprintf("unclosed/%d", pairs), func(b *testing.B) {
			var set bracketSet
			for b.Loop() {
				readBracket(&set, closed[:len(closed)-1], 0)
			}
		})
	}
}
