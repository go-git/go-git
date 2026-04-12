package compat_test

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportStorerRecordsCompatHashMappingImmediately(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)
	importer := compat.NewImportStorer(base, tr)

	compatBlob := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatBlob.SetType(plumbing.BlobObject)
	compatBlob.SetSize(int64(len("hello compat\n")))
	w, err := compatBlob.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello compat\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	nativeHash, err := importer.SetEncodedObject(compatBlob)
	require.NoError(t, err)

	mapped, err := mapping.CompatToNative(compatBlob.Hash())
	require.NoError(t, err)
	assert.Equal(t, nativeHash, mapped)

	obj, err := base.EncodedObject(plumbing.BlobObject, nativeHash)
	require.NoError(t, err)
	assert.Equal(t, nativeHash, obj.Hash())
}

func TestImportStorerImportsTopologicalCommitChain(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)
	importer := compat.NewImportStorer(base, tr)

	compatBlob := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatBlob.SetType(plumbing.BlobObject)
	compatBlob.SetSize(int64(len("hello compat\n")))
	w, err := compatBlob.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello compat\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	nativeBlob, err := importer.SetEncodedObject(compatBlob)
	require.NoError(t, err)

	nativeTreeContent := append([]byte("100644 file.txt\x00"), nativeBlob.Bytes()[:format.SHA256.Size()]...)
	compatTreeContent, err := tr.ReverseTranslateContent(plumbing.TreeObject, nativeTreeContent)
	require.NoError(t, err)

	compatTree := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatTree.SetType(plumbing.TreeObject)
	compatTree.SetSize(int64(len(compatTreeContent)))
	w, err = compatTree.Writer()
	require.NoError(t, err)
	_, err = w.Write(compatTreeContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	nativeTree, err := importer.SetEncodedObject(compatTree)
	require.NoError(t, err)

	nativeCommitContent := []byte("tree " + nativeTree.String() + "\n" +
		"author Compat Test <compat@example.com> 1700000000 +0000\n" +
		"committer Compat Test <compat@example.com> 1700000000 +0000\n\n" +
		"compat import\n")
	compatCommitContent, err := tr.ReverseTranslateContent(plumbing.CommitObject, nativeCommitContent)
	require.NoError(t, err)

	compatCommit := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatCommit.SetType(plumbing.CommitObject)
	compatCommit.SetSize(int64(len(compatCommitContent)))
	w, err = compatCommit.Writer()
	require.NoError(t, err)
	_, err = w.Write(compatCommitContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	nativeCommit, err := importer.SetEncodedObject(compatCommit)
	require.NoError(t, err)

	mappedTree, err := mapping.CompatToNative(compatTree.Hash())
	require.NoError(t, err)
	assert.Equal(t, nativeTree, mappedTree)

	mappedCommit, err := mapping.CompatToNative(compatCommit.Hash())
	require.NoError(t, err)
	assert.Equal(t, nativeCommit, mappedCommit)

	commitObj, err := base.EncodedObject(plumbing.CommitObject, nativeCommit)
	require.NoError(t, err)
	reader, err := commitObj.Reader()
	require.NoError(t, err)
	defer reader.Close()

	content := new(bytes.Buffer)
	_, err = content.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, string(nativeCommitContent), content.String())
}

func TestImportStorerFinalizeImportsDeferredCommitChain(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)
	importer := compat.NewImportStorer(base, tr)

	compatBlob := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatBlob.SetType(plumbing.BlobObject)
	compatBlob.SetSize(int64(len("hello compat\n")))
	w, err := compatBlob.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello compat\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	compatBlobHash := compatBlob.Hash()
	nativeBlobHash, err := tr.ComputeNativeHash(plumbing.BlobObject, []byte("hello compat\n"))
	require.NoError(t, err)

	nativeTreeContent := append([]byte("100644 file.txt\x00"), nativeBlobHash.Bytes()[:format.SHA256.Size()]...)
	compatTreeContent := append([]byte("100644 file.txt\x00"), compatBlobHash.Bytes()[:format.SHA1.Size()]...)

	compatTree := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatTree.SetType(plumbing.TreeObject)
	compatTree.SetSize(int64(len(compatTreeContent)))
	w, err = compatTree.Writer()
	require.NoError(t, err)
	_, err = w.Write(compatTreeContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	compatTreeHash := compatTree.Hash()
	nativeTreeHash, err := tr.ComputeNativeHash(plumbing.TreeObject, nativeTreeContent)
	require.NoError(t, err)

	compatCommitContent := []byte("tree " + compatTreeHash.String() + "\n" +
		"author Compat Test <compat@example.com> 1700000000 +0000\n" +
		"committer Compat Test <compat@example.com> 1700000000 +0000\n\n" +
		"compat import\n")

	compatCommit := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatCommit.SetType(plumbing.CommitObject)
	compatCommit.SetSize(int64(len(compatCommitContent)))
	w, err = compatCommit.Writer()
	require.NoError(t, err)
	_, err = w.Write(compatCommitContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = importer.SetEncodedObject(compatCommit)
	require.NoError(t, err)
	_, err = importer.SetEncodedObject(compatTree)
	require.NoError(t, err)
	_, err = importer.SetEncodedObject(compatBlob)
	require.NoError(t, err)

	require.NoError(t, importer.Finalize())

	mappedBlob, err := mapping.CompatToNative(compatBlobHash)
	require.NoError(t, err)
	assert.Equal(t, nativeBlobHash, mappedBlob)

	mappedTree, err := mapping.CompatToNative(compatTreeHash)
	require.NoError(t, err)
	assert.Equal(t, nativeTreeHash, mappedTree)

	mappedCommit, err := mapping.CompatToNative(compatCommit.Hash())
	require.NoError(t, err)

	commitObj, err := base.EncodedObject(plumbing.CommitObject, mappedCommit)
	require.NoError(t, err)
	assert.Equal(t, mappedCommit, commitObj.Hash())
}

func TestImportStorerReturnsCompatHashForDeferredObject(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)
	importer := compat.NewImportStorer(base, tr)

	compatTreeContent := append([]byte("100644 file.txt\x00"), plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes()[:format.SHA1.Size()]...)
	compatTree := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatTree.SetType(plumbing.TreeObject)
	compatTree.SetSize(int64(len(compatTreeContent)))
	w, err := compatTree.Writer()
	require.NoError(t, err)
	_, err = w.Write(compatTreeContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	returnedHash, err := importer.SetEncodedObject(compatTree)
	require.NoError(t, err)
	assert.Equal(t, compatTree.Hash(), returnedHash)

	_, err = mapping.CompatToNative(compatTree.Hash())
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}

func TestImportStorerExposesPendingCompatObjects(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)
	importer := compat.NewImportStorer(base, tr)

	compatTreeContent := append([]byte("100644 file.txt\x00"), plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes()[:format.SHA1.Size()]...)
	compatTree := plumbing.NewMemoryObject(plumbing.FromObjectFormat(format.SHA1))
	compatTree.SetType(plumbing.TreeObject)
	compatTree.SetSize(int64(len(compatTreeContent)))
	w, err := compatTree.Writer()
	require.NoError(t, err)
	_, err = w.Write(compatTreeContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = importer.SetEncodedObject(compatTree)
	require.NoError(t, err)

	assert.NoError(t, importer.HasEncodedObject(compatTree.Hash()))

	pendingObj, err := importer.EncodedObject(plumbing.TreeObject, compatTree.Hash())
	require.NoError(t, err)
	assert.Equal(t, compatTree.Hash(), pendingObj.Hash())

	size, err := importer.EncodedObjectSize(compatTree.Hash())
	require.NoError(t, err)
	assert.Equal(t, int64(len(compatTreeContent)), size)
}
