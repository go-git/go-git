package commitgraph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func encodedCommit(t *testing.T, content string) plumbing.EncodedObject {
	t.Helper()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.CommitObject)
	if _, err := obj.Write([]byte(content)); err != nil {
		t.Fatalf("write commit content: %v", err)
	}
	return obj
}

const (
	treeHex    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	parent1Hex = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	parent2Hex = "cccccccccccccccccccccccccccccccccccccccc"
)

func TestDecodeCommitBaseFields(t *testing.T) {
	t.Parallel()

	content := "tree " + treeHex + "\n" +
		"parent " + parent1Hex + "\n" +
		"parent " + parent2Hex + "\n" +
		"author Alice <alice@example.com> 1136239445 +0100\n" +
		"committer Bob <bob@example.com> 1136300000 -0500\n" +
		"\n" +
		"commit message body\n"

	obj := encodedCommit(t, content)
	c, err := DecodeCommit(obj)
	require.NoError(t, err)

	require.Equal(t, obj.Hash(), c.ID())
	require.Equal(t, plumbing.NewHash(treeHex), c.Tree())
	require.Equal(t, []plumbing.Hash{
		plumbing.NewHash(parent1Hex),
		plumbing.NewHash(parent2Hex),
	}, c.Parents())

	require.Equal(t, int64(1136300000), c.When().Unix())
	_, committerOffset := c.When().Zone()
	require.Equal(t, -5*60*60, committerOffset)

	require.Equal(t, int64(1136239445), c.AuthorWhen().Unix())
	_, authorOffset := c.AuthorWhen().Zone()
	require.Equal(t, 1*60*60, authorOffset)
}

func TestDecodeCommitRootCommit(t *testing.T) {
	t.Parallel()

	content := "tree " + treeHex + "\n" +
		"author Alice <alice@example.com> 1136239445 +0000\n" +
		"committer Bob <bob@example.com> 1136300000 +0000\n" +
		"\n" +
		"root\n"

	c, err := DecodeCommit(encodedCommit(t, content))
	require.NoError(t, err)

	require.Equal(t, plumbing.NewHash(treeHex), c.Tree())
	require.Empty(t, c.Parents())
	require.Equal(t, int64(1136300000), c.When().Unix())
	require.Equal(t, int64(1136239445), c.AuthorWhen().Unix())
}

func TestDecodeCommitIgnoresTrailingHeadersAndBody(t *testing.T) {
	t.Parallel()

	content := "tree " + treeHex + "\n" +
		"parent " + parent1Hex + "\n" +
		"author Alice <alice@example.com> 1136239445 +0000\n" +
		"committer Bob <bob@example.com> 1136300000 +0000\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n" +
		" \n" +
		" iQEcBAABCAAGBQJ...\n" +
		" -----END PGP SIGNATURE-----\n" +
		"mergetag object dddddddddddddddddddddddddddddddddddddddd\n" +
		" type commit\n" +
		" tag v1\n" +
		"custom-header some value\n" +
		"\n" +
		"message body that must never be parsed\n"

	c, err := DecodeCommit(encodedCommit(t, content))
	require.NoError(t, err)

	require.Equal(t, plumbing.NewHash(treeHex), c.Tree())
	require.Equal(t, []plumbing.Hash{plumbing.NewHash(parent1Hex)}, c.Parents())
	require.Equal(t, int64(1136300000), c.When().Unix())
	require.Equal(t, int64(1136239445), c.AuthorWhen().Unix())
}

func TestDecodeCommitRejectsNonCommit(t *testing.T) {
	t.Parallel()

	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.BlobObject)
	_, _ = obj.Write([]byte("not a commit"))

	_, err := DecodeCommit(obj)
	require.ErrorIs(t, err, object.ErrUnsupportedObject)
}

func TestDecodeCommitMissingTree(t *testing.T) {
	t.Parallel()

	content := "author Alice <alice@example.com> 1136239445 +0000\n" +
		"committer Bob <bob@example.com> 1136300000 +0000\n"

	_, err := DecodeCommit(encodedCommit(t, content))
	require.Error(t, err)
}

func TestDecodeCommitInvalidTreeHash(t *testing.T) {
	t.Parallel()

	content := "tree zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz\n" +
		"author Alice <alice@example.com> 1136239445 +0000\n" +
		"committer Bob <bob@example.com> 1136300000 +0000\n"

	_, err := DecodeCommit(encodedCommit(t, content))
	require.Error(t, err)
}

func TestDecodeCommitLongHeaderLines(t *testing.T) {
	t.Parallel()

	// Names longer than the bufio buffer (4096 bytes) force the line reader
	// past a single buffered slice; the base fields must still decode.
	longName := strings.Repeat("a", 9000)
	content := "tree " + treeHex + "\n" +
		"parent " + parent1Hex + "\n" +
		"author " + longName + " <a@example.com> 1136239445 +0100\n" +
		"committer " + longName + " <b@example.com> 1136300000 -0500\n" +
		"\n" +
		"body\n"

	c, err := DecodeCommit(encodedCommit(t, content))
	require.NoError(t, err)

	require.Equal(t, plumbing.NewHash(treeHex), c.Tree())
	require.Equal(t, []plumbing.Hash{plumbing.NewHash(parent1Hex)}, c.Parents())
	require.Equal(t, int64(1136300000), c.When().Unix())
	_, committerOffset := c.When().Zone()
	require.Equal(t, -5*60*60, committerOffset)
	require.Equal(t, int64(1136239445), c.AuthorWhen().Unix())
}
