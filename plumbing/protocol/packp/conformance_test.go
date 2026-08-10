package packp

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// fetchResponse assembles a fetch response from raw section lines. A line of
// "0001" emits a delim-pkt and "0000" a flush-pkt; anything else is written as
// a pkt-line.
func fetchResponse(lines ...string) []byte {
	var buf bytes.Buffer
	for _, l := range lines {
		switch l {
		case "0001":
			pktline.WriteDelim(&buf)
		case "0000":
			pktline.WriteFlush(&buf)
		default:
			pktline.WriteString(&buf, l+"\n")
		}
	}
	return buf.Bytes()
}

func TestFetchOutputDecodeConformance(t *testing.T) {
	t.Parallel()

	const oid = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	cases := []struct {
		name  string
		lines []string
	}{
		{
			name:  "repeated section",
			lines: []string{"shallow-info", "shallow " + oid, "0001", "shallow-info", "shallow " + oid, "0001", "packfile", "0000"},
		},
		{
			name:  "out-of-order sections",
			lines: []string{"wanted-refs", oid + " refs/heads/main", "0001", "shallow-info", "shallow " + oid, "0001", "packfile", "0000"},
		},
		{
			name:  "unknown section header",
			lines: []string{"bogus-section", "data", "0001", "packfile", "0000"},
		},
		{
			name:  "unknown acknowledgments line",
			lines: []string{"acknowledgments", "bogus", "0000"},
		},
		{
			name:  "unknown shallow-info line",
			lines: []string{"shallow-info", "bogus", "0001", "packfile", "0000"},
		},
		{
			name:  "ready without delimiter",
			lines: []string{"acknowledgments", "ACK " + oid, "ready", "0000"},
		},
		{
			name:  "acknowledgments without ready followed by delim",
			lines: []string{"acknowledgments", "NAK", "0001", "packfile", "0000"},
		},
		{
			name:  "metadata section without packfile",
			lines: []string{"shallow-info", "shallow " + oid, "0000"},
		},
		{
			name:  "sections then flush without packfile",
			lines: []string{"acknowledgments", "ACK " + oid, "ready", "0001", "0000"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := &FetchOutput{}
			err := out.Decode(bytes.NewReader(fetchResponse(tc.lines...)))
			require.Error(t, err)
			var mre *MalformedResponseError
			assert.True(t, errors.As(err, &mre), "want MalformedResponseError, got %T: %v", err, err)
		})
	}
}

func TestFetchOutputDecodeConformantShapesAccepted(t *testing.T) {
	t.Parallel()

	const oid = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	cases := []struct {
		name  string
		lines []string
	}{
		{name: "negotiation round (NAK flush)", lines: []string{"acknowledgments", "NAK", "0000"}},
		{name: "clone packfile only", lines: []string{"packfile", "0000"}},
		{name: "ready then packfile", lines: []string{"acknowledgments", "ACK " + oid, "ready", "0001", "packfile", "0000"}},
		{name: "shallow-info before packfile", lines: []string{"shallow-info", "shallow " + oid, "0001", "packfile", "0000"}},
		{name: "empty response", lines: []string{"0000"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := &FetchOutput{}
			require.NoError(t, out.Decode(bytes.NewReader(fetchResponse(tc.lines...))))
		})
	}
}

func TestLsRefsArgsDecodeTooManyPrefixes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	for range tooManyRefPrefixes + 1 {
		pktline.Writef(&buf, "ref-prefix refs/heads/x\n")
	}
	pktline.WriteFlush(&buf)

	var args LsRefsArgs
	require.NoError(t, args.Decode(&buf))
	require.Nil(t, args.RefPrefixes,
		"more than TOO_MANY_PREFIXES ref-prefixes must fall back to advertising all refs")
}

// TestFetchArgsDecodeTooManyWants mutates the package-wide maxSectionLines, so
// it cannot run in parallel with other tests that read it.
//
//nolint:paralleltest // mutates the shared maxSectionLines cap
func TestFetchArgsDecodeTooManyWants(t *testing.T) {
	old := maxSectionLines
	maxSectionLines = 3
	defer func() { maxSectionLines = old }()

	var buf bytes.Buffer
	for range maxSectionLines + 2 {
		pktline.WriteString(&buf, "want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
	}
	pktline.WriteFlush(&buf)

	var args FetchArgs
	err := args.Decode(&buf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too many want")
}

// TestFetchOutputDecodeTooManyACKs mutates the package-wide maxSectionLines, so
// it cannot run in parallel with other tests that read it.
//
//nolint:paralleltest // mutates the shared maxSectionLines cap
func TestFetchOutputDecodeTooManyACKs(t *testing.T) {
	old := maxSectionLines
	maxSectionLines = 2
	defer func() { maxSectionLines = old }()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "acknowledgments\n")
	for range maxSectionLines + 2 {
		pktline.WriteString(&buf, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
	}
	pktline.WriteFlush(&buf)

	out := &FetchOutput{}
	err := out.Decode(&buf)
	require.Error(t, err)
	var mre *MalformedResponseError
	require.True(t, errors.As(err, &mre), "want MalformedResponseError, got %T", err)
}

// TestFetchOutputDecodeTooManyPackfileURIs mutates the package-wide
// maxSectionLines, so it cannot run in parallel with other tests that read it.
//
//nolint:paralleltest // mutates the shared maxSectionLines cap
func TestFetchOutputDecodeTooManyPackfileURIs(t *testing.T) {
	old := maxSectionLines
	maxSectionLines = 2
	defer func() { maxSectionLines = old }()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "packfile-uris\n")
	for range maxSectionLines + 2 {
		pktline.WriteString(&buf, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 https://example/p.pack\n")
	}
	pktline.WriteFlush(&buf)

	out := &FetchOutput{}
	err := out.Decode(&buf)
	require.Error(t, err)
	var mre *MalformedResponseError
	require.True(t, errors.As(err, &mre), "want MalformedResponseError, got %T", err)
	require.Contains(t, err.Error(), "too many packfile-uris")
}

// TestFetchArgsDecodeTooManyDeepenNot mutates the package-wide maxSectionLines,
// so it cannot run in parallel with other tests that read it.
//
//nolint:paralleltest // mutates the shared maxSectionLines cap
func TestFetchArgsDecodeTooManyDeepenNot(t *testing.T) {
	old := maxSectionLines
	maxSectionLines = 3
	defer func() { maxSectionLines = old }()

	var buf bytes.Buffer
	for range maxSectionLines + 2 {
		pktline.WriteString(&buf, "deepen-not refs/heads/x\n")
	}
	pktline.WriteFlush(&buf)

	var args FetchArgs
	err := args.Decode(&buf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too many deepen-not")
}

func TestFetchOutputDecodeTruncatedAfterMetadata(t *testing.T) {
	t.Parallel()

	const oid = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	// shallow-info commits the response to the packfile shape; a premature EOF
	// before the packfile section is a truncated response, not a clean end, and
	// must be reported rather than silently accepted.
	out := &FetchOutput{}
	err := out.Decode(bytes.NewReader(fetchResponse("shallow-info", "shallow "+oid, "0001")))
	require.Error(t, err)
	var mre *MalformedResponseError
	require.True(t, errors.As(err, &mre), "want MalformedResponseError, got %T: %v", err, err)
}
