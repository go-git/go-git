package convert

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

func TestGetStat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    []byte
		expected Stat
	}{
		{
			input: []byte("ABCDEFG"),
			expected: Stat{
				Printable: 7,
			},
		},
		{
			input: []byte("\r\n\r\n\r\n"),
			expected: Stat{
				CRLF: 3,
			},
		},
		{
			input: []byte("\r\r\n"),
			expected: Stat{
				LoneCR: 1,
				CRLF:   1,
			},
		},
		{
			input: []byte("\r\n\r"),
			expected: Stat{
				LoneCR: 1,
				CRLF:   1,
			},
		},
		{
			input: []byte("NUL\x00"),
			expected: Stat{
				Printable: 3,
				NUL:       1,
			},
		},
		{
			input: []byte{127}, // DEL
			expected: Stat{
				NonPrintable: 1,
			},
		},
		{
			input: []byte{'\n', '\032'}, // LF, EOF
			expected: Stat{
				LoneLF:       1,
				NonPrintable: 0,
			},
		},
		{
			input:    nil,
			expected: Stat{},
		},
		{
			input:    []byte{0x1a},
			expected: Stat{},
		},
		{
			input: []byte{0x1a, 0x1a},
			expected: Stat{
				NonPrintable: 1,
			},
		},
		{
			input: []byte{0x1a, 'a'},
			expected: Stat{
				Printable:    1,
				NonPrintable: 1,
			},
		},
		{
			input: []byte("a\r"),
			expected: Stat{
				Printable: 1,
				LoneCR:    1,
			},
		},
		{
			input: []byte("\n\r"),
			expected: Stat{
				LoneLF: 1,
				LoneCR: 1,
			},
		},
		{
			input: []byte("a\nb\r\nc\rd"),
			expected: Stat{
				Printable: 4,
				LoneLF:    1,
				CRLF:      1,
				LoneCR:    1,
			},
		},
		{
			input: []byte("\b\t\f\x1b"),
			expected: Stat{
				Printable: 4,
			},
		},
		{
			input: []byte{1, 2, 3, 4, 5, 6, 7, 11, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 28, 29, 30, 31, 'a'},
			expected: Stat{
				Printable:    1,
				NonPrintable: 25,
			},
		},
		{
			input: []byte{' ', '~', 0x80, 0xff},
			expected: Stat{
				Printable: 4,
			},
		},
	}

	for idx, test := range tests {
		t.Run(fmt.Sprintf("#%d %s", idx, test.input), func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(test.input)

			stat, err := GetStat(r)
			require.NoError(t, err)

			assert.Equal(t, test.expected, stat)
		})
	}
}

func TestIsBinary(t *testing.T) {
	t.Parallel()
	stat := Stat{}
	assert.False(t, stat.IsBinary())

	stat = Stat{NUL: 1}
	assert.True(t, stat.IsBinary())

	stat = Stat{LoneCR: 1}
	assert.True(t, stat.IsBinary())

	stat = Stat{Printable: 1, NonPrintable: 1}
	assert.True(t, stat.IsBinary())

	stat = Stat{Printable: 1 << 7, NonPrintable: 1}
	assert.False(t, stat.IsBinary())
}

// A CR and the LF after it can arrive in separate reads, and the final
// Ctrl-Z in a read of its own.
func TestGetStatAcrossReads(t *testing.T) {
	t.Parallel()

	input := []byte("a\r\nb\rc\n\x00\x01\r\n\x1a")
	want := Stat{Printable: 3, CRLF: 2, LoneCR: 1, LoneLF: 1, NUL: 1, NonPrintable: 1}

	stat, err := GetStat(iotest.OneByteReader(bytes.NewReader(input)))
	require.NoError(t, err)
	assert.Equal(t, want, stat)

	stat, err = GetStat(iotest.DataErrReader(bytes.NewReader(input)))
	require.NoError(t, err)
	assert.Equal(t, want, stat)
}

func TestGetStatLargeInput(t *testing.T) {
	t.Parallel()

	const pairs = 100_000
	input := bytes.Repeat([]byte("x\r\n"), pairs)
	input = append(input, '\r')

	stat, err := GetStat(bytes.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, Stat{Printable: pairs, CRLF: pairs, LoneCR: 1}, stat)
}

func TestGetStatReadError(t *testing.T) {
	t.Parallel()

	errRead := errors.New("read failed")
	r := io.MultiReader(strings.NewReader("text\n"), iotest.ErrReader(errRead))

	stat, err := GetStat(r)
	require.ErrorIs(t, err, errRead)
	assert.Equal(t, Stat{}, stat)
}

func TestIsBinaryThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stat Stat
		want bool
	}{
		{Stat{NonPrintable: 1}, true},
		{Stat{Printable: 127, NonPrintable: 1}, true},
		{Stat{Printable: 128, NonPrintable: 1}, false},
		{Stat{Printable: 255, NonPrintable: 1}, false},
		{Stat{Printable: 255, NonPrintable: 2}, true},
		{Stat{Printable: 256, NonPrintable: 2}, false},
		{Stat{Printable: 256, NonPrintable: 3}, true},
		{Stat{LoneLF: 5, CRLF: 5, Printable: 5}, false},
		{Stat{Printable: 1 << 20, NUL: 1}, true},
		{Stat{Printable: 1 << 20, LoneCR: 1}, true},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.stat.IsBinary(), "%+v", tc.stat)
	}
}

// TestStatMatchesGit adds files around each boundary of the text
// heuristic to an index and compares GetStat and IsBinary with the
// classification git ls-files --eol prints for the staged content.
func TestStatMatchesGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("oracle disabled: git not found: %v", err)
	}

	text := func(n int) []byte { return bytes.Repeat([]byte{'a'}, n) }
	samples := map[string][]byte{
		"empty":               nil,
		"no-eol":              []byte("abc"),
		"lf":                  []byte("a\nb\n"),
		"crlf":                []byte("a\r\nb\r\n"),
		"mixed":               []byte("a\r\nb\n"),
		"cr-cr-lf":            []byte("a\r\r\nb"),
		"ends-in-cr":          []byte("a\r"),
		"only-ctrl-z":         {0x1a},
		"lf-ctrl-z":           {'\n', 0x1a},
		"127-text-ctrl-z":     append(text(127), 0x1a),
		"ctrl-z-127-text":     append([]byte{0x1a}, text(127)...),
		"127-text-2-ctrl-z":   append(text(127), 0x1a, 0x1a),
		"128-text-2-ctrl-z":   append(text(128), 0x1a, 0x1a),
		"127-high-1-control":  append(bytes.Repeat([]byte{0xff}, 127), 1),
		"128-high-1-control":  append(bytes.Repeat([]byte{0xff}, 128), 1),
		"255-text-1-control":  append(text(255), 1),
		"255-text-2-control":  append(text(255), 1, 1),
		"256-text-2-control":  append(text(256), 1, 1),
		"256-text-3-control":  append(text(256), 1, 1, 1),
		"nul-after-much-text": append(bytes.Repeat([]byte("line of text\n"), 20_000), 0),
		"cr-after-much-text":  append(bytes.Repeat([]byte("line of text\r\n"), 20_000), '\r', 'a'),
	}
	// One byte of each value in the range git treats specially, after
	// text one short of what makes a single non-printable byte tolerable,
	// and after just enough of it.
	for _, b := range byteRange(0, 32, 127, 128, 255) {
		samples[fmt.Sprintf("127-text-byte-%d", b)] = append(text(127), b)
		samples[fmt.Sprintf("128-text-byte-%d", b)] = append(text(128), b)
		samples[fmt.Sprintf("byte-%d-127-text", b)] = append([]byte{b}, text(127)...)
	}

	dir := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()

		cmd := gitenv.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		require.NoError(t, err, "git %s", strings.Join(args, " "))
		return string(out)
	}

	runGit("init", "-q", ".")
	for name, content := range samples {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o600))
	}
	runGit("-c", "core.autocrlf=false", "add", ".")

	seen := 0
	for line := range strings.Lines(runGit("ls-files", "--eol")) {
		// i/<index eol> w/<worktree eol> attr/<attributes> TAB <path>
		info, name, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		require.True(t, ok, "unexpected ls-files --eol line %q", line)
		gitEOL := strings.TrimPrefix(strings.Fields(info)[0], "i/")
		content, ok := samples[name]
		require.True(t, ok, "unexpected path %q", name)
		seen++

		stat, err := GetStat(bytes.NewReader(content))
		require.NoError(t, err)

		var eol string
		switch {
		case stat.IsBinary():
			eol = "-text"
		case stat.CRLF > 0 && stat.LoneLF > 0:
			eol = "mixed"
		case stat.CRLF > 0:
			eol = "crlf"
		case stat.LoneLF > 0:
			eol = "lf"
		default:
			eol = "none"
		}
		assert.Equal(t, gitEOL, eol, "%s: %+v", name, stat)
	}
	assert.Equal(t, len(samples), seen)
}

// byteRange returns every byte in [from, to) followed by extra.
func byteRange(from, to int, extra ...byte) []byte {
	var bs []byte
	for b := from; b < to; b++ {
		bs = append(bs, byte(b))
	}
	return append(bs, extra...)
}
