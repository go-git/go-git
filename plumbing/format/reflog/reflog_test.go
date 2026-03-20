package reflog

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

const testReflog = "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author Name <author@example.com> 1234567890 +0000\tcommit (initial): Initial commit\n" +
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb Another Author <another@example.com> 1234567891 +0100\tcommit: Second commit\n"

func TestDecode(t *testing.T) {
	t.Parallel()

	entries, err := Decode(strings.NewReader(testReflog))
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, plumbing.ZeroHash, entries[0].OldHash)
	assert.Equal(t, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), entries[0].NewHash)
	assert.Equal(t, "Author Name", entries[0].Committer.Name)
	assert.Equal(t, "author@example.com", entries[0].Committer.Email)
	assert.Equal(t, "commit (initial): Initial commit", entries[0].Message)

	assert.Equal(t, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), entries[1].OldHash)
	assert.Equal(t, plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), entries[1].NewHash)
	assert.Equal(t, "Another Author", entries[1].Committer.Name)
	assert.Equal(t, "another@example.com", entries[1].Committer.Email)
	assert.Equal(t, "commit: Second commit", entries[1].Message)
}

func TestDecodeEmpty(t *testing.T) {
	t.Parallel()

	entries, err := Decode(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestDecodeNoMessage(t *testing.T) {
	t.Parallel()

	line := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 +0000\n"
	entries, err := Decode(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "Author", entries[0].Committer.Name)
	assert.Equal(t, "a@b.com", entries[0].Committer.Email)
	assert.Equal(t, "", entries[0].Message)
}

func TestDecodeInvalidTimestamp(t *testing.T) {
	t.Parallel()

	line := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> notanumber +0000\n"
	_, err := Decode(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timestamp seconds")
}

func TestDecodeInvalidTimezone(t *testing.T) {
	t.Parallel()

	line := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 XXXX\n"
	_, err := Decode(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timezone")
}

func TestDecodeMissingTimestamp(t *testing.T) {
	t.Parallel()

	line := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com>\n"
	_, err := Decode(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing timestamp")
}

func TestEncode(t *testing.T) {
	t.Parallel()

	e := &Entry{
		OldHash: plumbing.ZeroHash,
		NewHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Committer: Signature{
			Name:  "Author Name",
			Email: "author@example.com",
			When:  time.Unix(1234567890, 0).UTC(),
		},
		Message: "commit (initial): Initial commit",
	}

	var buf bytes.Buffer
	err := Encode(&buf, e)
	require.NoError(t, err)

	expected := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author Name <author@example.com> 1234567890 +0000\tcommit (initial): Initial commit\n"
	assert.Equal(t, expected, buf.String())
}

func TestEncodeNoMessage(t *testing.T) {
	t.Parallel()

	e := &Entry{
		OldHash: plumbing.ZeroHash,
		NewHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Committer: Signature{
			Name:  "Author",
			Email: "a@b.com",
			When:  time.Unix(1234567890, 0).UTC(),
		},
	}

	var buf bytes.Buffer
	err := Encode(&buf, e)
	require.NoError(t, err)

	expected := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 +0000\n"
	assert.Equal(t, expected, buf.String())
}

func TestEncodeMessageNormalization(t *testing.T) {
	t.Parallel()

	e := &Entry{
		OldHash: plumbing.ZeroHash,
		NewHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Committer: Signature{
			Name:  "Author",
			Email: "a@b.com",
			When:  time.Unix(1234567890, 0).UTC(),
		},
		Message: "commit:  multiple   spaces\nand\nnewlines  ",
	}

	var buf bytes.Buffer
	err := Encode(&buf, e)
	require.NoError(t, err)

	expected := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 +0000\tcommit: multiple spaces and newlines\n"
	assert.Equal(t, expected, buf.String())
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	original := &Entry{
		OldHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		NewHash: plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		Committer: Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Unix(1700000000, 0).UTC(),
		},
		Message: "checkout: moving from main to feature",
	}

	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, original))

	entries, err := Decode(&buf)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	decoded := entries[0]
	assert.Equal(t, original.OldHash, decoded.OldHash)
	assert.Equal(t, original.NewHash, decoded.NewHash)
	assert.Equal(t, original.Committer.Name, decoded.Committer.Name)
	assert.Equal(t, original.Committer.Email, decoded.Committer.Email)
	assert.Equal(t, original.Committer.When.Unix(), decoded.Committer.When.Unix())
	assert.Equal(t, original.Message, decoded.Message)
}

func TestDecodeRealGitReflog(t *testing.T) {
	t.Parallel()

	line := "0000000000000000000000000000000000000000 2083cf940afa6b2cf04bad67f152ab56514e68bc Stefan Haubold <stefan@haubi.com> 1772814283 +0100\tclone: from github.com:go-git/go-git.git\n"
	entries, err := Decode(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "Stefan Haubold", entries[0].Committer.Name)
	assert.Equal(t, "stefan@haubi.com", entries[0].Committer.Email)
	assert.Equal(t, "clone: from github.com:go-git/go-git.git", entries[0].Message)
	assert.Equal(t, "2083cf940afa6b2cf04bad67f152ab56514e68bc", entries[0].NewHash.String())
}

func TestDecodeInvalidOldHash(t *testing.T) {
	t.Parallel()

	line := "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 +0000\tcommit: test\n"
	_, err := Decode(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid old hash")
}

func TestDecodeInvalidNewHash(t *testing.T) {
	t.Parallel()

	line := "0000000000000000000000000000000000000000 not-a-valid-hash Author <a@b.com> 1234567890 +0000\tcommit: test\n"
	_, err := Decode(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid new hash")
}

func TestDecodeTimestampExtraFields(t *testing.T) {
	t.Parallel()

	// Extra fields after timezone should be rejected to catch missing TAB separators
	line := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 +0000 extrastuff\n"
	_, err := Decode(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timestamp")
}

func TestDecodeLongLine(t *testing.T) {
	t.Parallel()

	message := strings.Repeat("a", 70*1024)
	line := "0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa Author <a@b.com> 1234567890 +0000\t" + message + "\n"

	entries, err := Decode(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, message, entries[0].Message)
}
