package packp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/utils/trace"
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
			// The empty string is not a valid reference name, so a line with
			// no name is skipped like any other the decoder cannot use. It
			// sits one carriage return from "name is only carriage returns"
			// and trims to the same string as "peel suffix with no name", and
			// all three take the same exit.
			name:     "empty reference name",
			input:    sha1Head + "\t\n",
			wantRefs: nil,
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
		{
			name:     "line terminated with CRLF",
			input:    sha1Head + "\trefs/heads/master\r\n",
			wantRefs: []string{"refs/heads/master"},
		},
		{
			// A body that has been through a CRLF conversion twice. The
			// scanner drops one carriage return and the other stays in the
			// name, which the name rule refuses. Losing the reference is what
			// upstream does with it, and it is what keeps the name from
			// depending on how many conversions the body has been through.
			name:     "line terminated with two carriage returns",
			input:    sha1Head + "\trefs/heads/master\r\r\n",
			wantRefs: nil,
		},
		{
			name:     "carriage returns at the end of the body",
			input:    sha1Head + "\trefs/heads/master\r\r",
			wantRefs: nil,
		},
		{
			// Reduced from an OSS-Fuzz reproducer. The scanner drops one
			// carriage return and the name is the other, which the name rule
			// refuses, so the body decodes to nothing rather than to a
			// reference this package cannot write back.
			name:     "name is only carriage returns",
			input:    sha1Head + "\t\r\r",
			wantRefs: nil,
		},
		{
			// A carriage return inside a name is not at the end of the line,
			// so the scanner leaves it and the name rule refuses it.
			name:     "carriage return inside a name",
			input:    sha1Head + "\trefs/heads/mas\rter\n",
			wantRefs: nil,
		},
		{
			name:     "name holds a space",
			input:    sha1Head + "\trefs/heads/ma ster\n",
			wantRefs: nil,
		},
		{
			name:     "name holds a control byte",
			input:    sha1Head + "\trefs/heads/mas\x00ter\n",
			wantRefs: nil,
		},
		{
			// git update-server-info writes names under refs/, and a name of
			// one component is not a valid reference name.
			name:     "single level name",
			input:    sha1Head + "\tmaster\n",
			wantRefs: nil,
		},
		{
			// The line an unusable name sits on is the only thing dropped.
			name: "unusable name among usable ones",
			input: sha1Head + "\trefs/heads/master\n" +
				sha1Head + "\trefs/heads/ma ster\n" +
				sha1Head + "\trefs/tags/v1\n",
			wantRefs: []string{"refs/heads/master", "refs/tags/v1"},
		},
		{
			// A peel suffix is stripped before the name is checked, so the
			// base has to carry the line on its own.
			name:     "peeled name with an unusable base",
			input:    sha1Head + "\trefs/heads/ma ster^{}\n",
			wantRefs: nil,
		},
		{
			name:     "peel suffix with no name",
			input:    sha1Head + "\t^{}\n",
			wantRefs: nil,
		},
		{
			// Only one suffix is stripped, so what is left still holds a "^".
			name:     "name peeled twice",
			input:    sha1Head + "\trefs/tags/v1^{}^{}\n",
			wantRefs: nil,
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

func TestInfoRefsDecodeTracesSkippedName(t *testing.T) { //nolint:paralleltest // modifies global trace configuration
	var logs bytes.Buffer
	previousTarget := trace.GetTarget()
	t.Cleanup(func() {
		trace.SetTarget(previousTarget)
		trace.SetLogger(log.New(os.Stderr, "", log.Ltime|log.Lmicroseconds|log.Lshortfile))
	})
	trace.SetLogger(log.New(&logs, "", 0))
	trace.SetTarget(trace.General)

	// What a hostile server can put in a name: an escape sequence that would
	// rewrite the terminal reading the log, and a NUL. %q is what makes them
	// inert, and it is what the other reference-name traces already use.
	const hostile = "refs/heads/\x1b[2Jma ster\x00"

	var refs InfoRefs
	err := refs.Decode(strings.NewReader(
		sha1Head + "\t" + hostile + "\n" +
			sha1Head + "\trefs/heads/master\n",
	))

	require.NoError(t, err, "one unusable name must not fail the advertisement")

	got := logs.String()
	assert.Contains(t, got, `"refs/heads/\x1b[2Jma ster\x00"`,
		"the name should be quoted, as every other reference name go-git traces is")
	assert.Contains(t, got, "line 1", "the trace should locate the skipped line")
	assert.NotContains(t, got, "\x1b", "no raw escape byte may reach the log")
	assert.NotContains(t, got, "\x00", "no raw control byte may reach the log")

	names := make([]string, 0, len(refs.References))
	for _, ref := range refs.References {
		names = append(names, ref.Name().String())
	}
	assert.Equal(t, []string{"refs/heads/master"}, names)
}

// TestInfoRefsDecodeNamelessLineCostsOneLine pins the continuity the name rule
// is there to give. A line carrying no usable name arrives in several
// spellings that sit one carriage return apart, and "^{}" trims to the same
// empty string as a bare tab. All of them have to leave the advertisement
// standing and cost their own line, or a body one byte from another decodes to
// nothing where its neighbour decodes to everything.
func TestInfoRefsDecodeNamelessLineCostsOneLine(t *testing.T) {
	t.Parallel()

	for _, nameless := range []string{"", "\r", "\r\r", "^{}", " ", "\t"} {
		t.Run(fmt.Sprintf("%q", nameless), func(t *testing.T) {
			t.Parallel()

			var refs InfoRefs
			err := refs.Decode(strings.NewReader(
				sha1Head + "\trefs/heads/main\n" +
					sha1Head + "\t" + nameless + "\n" +
					sha1Head + "\trefs/heads/other\n",
			))
			require.NoError(t, err, "a line with no usable name must not fail the advertisement")

			names := make([]string, 0, len(refs.References))
			for _, ref := range refs.References {
				names = append(names, ref.Name().String())
			}
			assert.Equal(t, []string{"refs/heads/main", "refs/heads/other"}, names)
		})
	}
}
