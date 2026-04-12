package testcompat

import (
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"
)

type EncodedObjectStore interface {
	NewEncodedObject() plumbing.EncodedObject
	SetEncodedObject(plumbing.EncodedObject) (plumbing.Hash, error)
}

func PopulateCompatChain(t *testing.T, sto EncodedObjectStore) (blobHash, treeHash, commitHash, tagHash plumbing.Hash) {
	t.Helper()

	blobContent := []byte("hello compat\n")
	blob := sto.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	blob.SetSize(int64(len(blobContent)))
	w, err := blob.Writer()
	require.NoError(t, err)
	_, err = w.Write(blobContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	blobHash, err = sto.SetEncodedObject(blob)
	require.NoError(t, err)

	treeContent := append([]byte("100644 hello.txt\x00"), blobHash.Bytes()...)
	tree := sto.NewEncodedObject()
	tree.SetType(plumbing.TreeObject)
	tree.SetSize(int64(len(treeContent)))
	w, err = tree.Writer()
	require.NoError(t, err)
	_, err = w.Write(treeContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	treeHash, err = sto.SetEncodedObject(tree)
	require.NoError(t, err)

	commitContent := []byte("tree " + treeHash.String() + "\n" +
		"author Compat Test <compat@example.com> 1700000000 +0000\n" +
		"committer Compat Test <compat@example.com> 1700000000 +0000\n" +
		"\n" +
		"Initial commit\n")
	commit := sto.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)
	commit.SetSize(int64(len(commitContent)))
	w, err = commit.Writer()
	require.NoError(t, err)
	_, err = w.Write(commitContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	commitHash, err = sto.SetEncodedObject(commit)
	require.NoError(t, err)

	tagContent := []byte("object " + commitHash.String() + "\n" +
		"type commit\n" +
		"tag v1.0\n" +
		"tagger Compat Test <compat@example.com> 1700000000 +0000\n" +
		"\n" +
		"compat tag\n")
	tag := sto.NewEncodedObject()
	tag.SetType(plumbing.TagObject)
	tag.SetSize(int64(len(tagContent)))
	w, err = tag.Writer()
	require.NoError(t, err)
	_, err = w.Write(tagContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	tagHash, err = sto.SetEncodedObject(tag)
	require.NoError(t, err)

	return blobHash, treeHash, commitHash, tagHash
}

func ReadEncodedObject(t *testing.T, obj plumbing.EncodedObject) []byte {
	t.Helper()

	r, err := obj.Reader()
	require.NoError(t, err)
	defer r.Close()

	content, err := io.ReadAll(r)
	require.NoError(t, err)
	return content
}
