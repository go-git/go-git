package packp

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sha1Head   = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	sha256Head = "6ecf0ef2c2dffb796033e5a02219af86ec6584e56ecf0ef2c2dffb796033e5a0"
)

func TestInfoRefsDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantRefs []string
		wantErr  bool
	}{
		{
			name:     "single sha1 reference",
			input:    sha1Head + "\trefs/heads/master\n",
			wantRefs: []string{"refs/heads/master"},
		},
		{
			name:     "single sha256 reference",
			input:    sha256Head + "\trefs/heads/master\n",
			wantRefs: []string{"refs/heads/master"},
		},
		{
			name: "several references",
			input: sha1Head + "\trefs/heads/master\n" +
				sha1Head + "\trefs/tags/v1\n",
			wantRefs: []string{"refs/heads/master", "refs/tags/v1"},
		},
		{
			name:     "peeled reference",
			input:    sha1Head + "\trefs/tags/v1^{}\n",
			wantRefs: []string{"refs/tags/v1^{}"},
		},
		{
			// A repository with no references. git update-server-info writes a
			// zero-byte file, and that must stay distinguishable from a body
			// that is not an info/refs at all.
			name:     "empty input",
			input:    "",
			wantRefs: nil,
		},
		{
			name:     "trailing blank line",
			input:    sha1Head + "\trefs/heads/master\n\n",
			wantRefs: []string{"refs/heads/master"},
		},
		{
			// plumbing.FromHex pads a short hex string rather than rejecting
			// it, so the length is what disqualifies this, not the parse.
			name:    "hash shorter than the hash size",
			input:   "deadbeef\trefs/heads/master\n",
			wantErr: true,
		},
		{
			name:    "hash longer than the hash size",
			input:   sha1Head + "ff\trefs/heads/master\n",
			wantErr: true,
		},
		{
			name:    "hash is not hexadecimal",
			input:   strings.Repeat("z", 40) + "\trefs/heads/master\n",
			wantErr: true,
		},
		{
			name:    "odd number of hex digits",
			input:   strings.Repeat("a", 41) + "\trefs/heads/master\n",
			wantErr: true,
		},
		{
			// A page minified onto one line. bufio.Scanner gives up on a line
			// this long, and that has to reach the caller as a malformed
			// advertisement like any other.
			name:    "line longer than the scanner can hold",
			input:   "<html>" + strings.Repeat("x", bufio.MaxScanTokenSize) + "</html>\n",
			wantErr: true,
		},
		{
			name:    "no tab",
			input:   "<html><body>nope</body></html>\n",
			wantErr: true,
		},
		{
			name:    "empty reference name",
			input:   sha1Head + "\t\n",
			wantErr: true,
		},
		{
			// The case that motivated this: markup whose indentation happens to
			// put valid-looking hex before a tab.
			name:    "html with hex before a tab",
			input:   "<html>\n\tabcdef\tSign in to continue\n</html>\n",
			wantErr: true,
		},
		{
			// A byte order mark ahead of an otherwise valid line.
			name:    "byte order mark",
			input:   "\ufeff" + sha1Head + "\trefs/heads/master\n",
			wantErr: true,
		},
		{
			// One malformed line fails the advertisement; the valid lines
			// around it do not rescue it.
			name:    "junk line before a valid one",
			input:   "<!-- injected -->\n" + sha1Head + "\trefs/heads/master\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var refs InfoRefs
			err := refs.Decode(strings.NewReader(tt.input))

			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidInfoRefs)
				return
			}

			require.NoError(t, err)
			names := make([]string, 0, len(refs.References))
			for _, ref := range refs.References {
				names = append(names, ref.Name().String())
			}
			assert.Equal(t, tt.wantRefs, nilIfEmpty(names))
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// TestInfoRefsDecodeErrorOmitsBody keeps server-controlled bytes out of the
// error. The HTTP transport decides separately whether a response body is safe
// to quote back, and an error carrying the line would bypass that.
func TestInfoRefsDecodeErrorOmitsBody(t *testing.T) {
	t.Parallel()

	const secret = "private_token=SECRET"

	var refs InfoRefs
	err := refs.Decode(strings.NewReader("<html>" + secret + "</html>\n"))

	require.ErrorIs(t, err, ErrInvalidInfoRefs)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, err.Error(), "<html>")
}

// TestInfoRefsDecodeErrorKeepsNoReferences covers what a rejected body leaves
// behind. The lines ahead of the offending one parse, and keeping them would
// hand the caller a ref list assembled from a body that is not one.
func TestInfoRefsDecodeErrorKeepsNoReferences(t *testing.T) {
	t.Parallel()

	var refs InfoRefs
	err := refs.Decode(strings.NewReader(
		sha1Head + "\trefs/heads/master\n<html>sign in to continue</html>\n",
	))

	require.ErrorIs(t, err, ErrInvalidInfoRefs)
	assert.Empty(t, refs.References,
		"a rejected advertisement must contribute no references")
}

func TestInfoRefsDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	in := sha1Head + "\trefs/heads/master\n" + sha1Head + "\trefs/tags/v1\n"

	var refs InfoRefs
	require.NoError(t, refs.Decode(strings.NewReader(in)))

	var out strings.Builder
	require.NoError(t, refs.Encode(&out))
	assert.Equal(t, in, out.String())
}

// TestInfoRefsDecodeReadError keeps a failed read apart from a malformed body.
// The bytes that did arrive may well have been a valid advertisement, so the
// read error is returned as itself.
func TestInfoRefsDecodeReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset by peer")

	var refs InfoRefs
	err := refs.Decode(io.MultiReader(
		strings.NewReader(sha1Head+"\trefs/heads/master\n"),
		iotest.ErrReader(readErr),
	))

	require.ErrorIs(t, err, readErr)
	assert.NotErrorIs(t, err, ErrInvalidInfoRefs)
}
