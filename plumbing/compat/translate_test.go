package compat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat/oidmap"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

func newTestTranslator() (*Translator, *oidmap.Memory) {
	m := oidmap.NewMemory()
	return NewTranslator(format.SHA1, format.SHA256, m), m
}

func makeEncodedObject(t *testing.T, objType plumbing.ObjectType, content []byte, f format.ObjectFormat) plumbing.EncodedObject {
	t.Helper()
	hasher := plumbing.FromObjectFormat(f)
	obj := plumbing.NewMemoryObject(hasher)
	obj.SetType(objType)
	obj.SetSize(int64(len(content)))
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return obj
}

func TestTranslatorObjectFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		native format.ObjectFormat
		compat format.ObjectFormat
	}{
		{
			name:   "sha1 native sha256 compat",
			native: format.SHA1,
			compat: format.SHA256,
		},
		{
			name:   "sha256 native sha1 compat",
			native: format.SHA256,
			compat: format.SHA1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := NewTranslator(tt.native, tt.compat, oidmap.NewMemory())
			assert.Equal(t, tt.native, tr.NativeObjectFormat())
			assert.Equal(t, tt.compat, tr.CompatObjectFormat())
		})
	}
}

func TestTranslateBlob(t *testing.T) {
	t.Parallel()

	tr, m := newTestTranslator()

	blobContent := []byte("hello world\n")
	obj := makeEncodedObject(t, plumbing.BlobObject, blobContent, format.SHA1)

	compatHash, err := tr.TranslateObject(obj)
	require.NoError(t, err)

	// The compat hash should be the SHA-256 of the same content.
	expectedHash, err := tr.ComputeCompatHash(plumbing.BlobObject, blobContent)
	require.NoError(t, err)
	assert.True(t, compatHash.Equal(expectedHash), "compat hash mismatch: got %s, want %s", compatHash, expectedHash)

	// Mapping should be recorded.
	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	got, err := m.ToCompat(obj.Hash())
	require.NoError(t, err)
	assert.True(t, got.Equal(compatHash))
}

func TestTranslateTree(t *testing.T) {
	t.Parallel()

	tr, m := newTestTranslator()

	// First, create a blob and translate it so its mapping exists.
	blobContent := []byte("file content")
	blobObj := makeEncodedObject(t, plumbing.BlobObject, blobContent, format.SHA1)
	_, err := tr.TranslateObject(blobObj)
	require.NoError(t, err)

	// Build a tree with one entry pointing to the blob.
	// Tree entry format: "<mode> <name>\0<20-byte-hash>"
	treeContent := make([]byte, 0, 16+len(blobObj.Hash().Bytes()))
	treeContent = append(treeContent, []byte("100644 test.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, blobObj.Hash().Bytes()...)

	treeObj := makeEncodedObject(t, plumbing.TreeObject, treeContent, format.SHA1)

	compatHash, err := tr.TranslateObject(treeObj)
	require.NoError(t, err)

	// Verify the mapping was recorded.
	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 2, count) // blob + tree
	got, err := m.ToCompat(treeObj.Hash())
	require.NoError(t, err)
	assert.True(t, got.Equal(compatHash))

	// Verify the compat hash is different from the native hash.
	assert.False(t, treeObj.Hash().Equal(compatHash))
}

func TestTranslateCommit(t *testing.T) {
	t.Parallel()

	tr, m := newTestTranslator()

	// Create and translate a blob, then a tree pointing to it.
	blobContent := []byte("content")
	blobObj := makeEncodedObject(t, plumbing.BlobObject, blobContent, format.SHA1)
	_, err := tr.TranslateObject(blobObj)
	require.NoError(t, err)

	treeContent := make([]byte, 0, 16+len(blobObj.Hash().Bytes()))
	treeContent = append(treeContent, []byte("100644 file.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, blobObj.Hash().Bytes()...)
	treeObj := makeEncodedObject(t, plumbing.TreeObject, treeContent, format.SHA1)
	_, err = tr.TranslateObject(treeObj)
	require.NoError(t, err)

	// Build a commit referencing the tree.
	commitText := "tree " + treeObj.Hash().String() + "\n" +
		"author Test User <test@example.com> 1234567890 +0000\n" +
		"committer Test User <test@example.com> 1234567890 +0000\n" +
		"\n" +
		"Initial commit\n"

	commitObj := makeEncodedObject(t, plumbing.CommitObject, []byte(commitText), format.SHA1)
	compatHash, err := tr.TranslateObject(commitObj)
	require.NoError(t, err)

	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 3, count) // blob + tree + commit
	got, err := m.ToCompat(commitObj.Hash())
	require.NoError(t, err)
	assert.True(t, got.Equal(compatHash))
}

func TestTranslateCommitWithParents(t *testing.T) {
	t.Parallel()

	tr, _ := newTestTranslator()

	// Create blob -> tree -> commit1 (root) -> commit2 (child).
	blobObj := makeEncodedObject(t, plumbing.BlobObject, []byte("data"), format.SHA1)
	_, err := tr.TranslateObject(blobObj)
	require.NoError(t, err)

	treeContent := make([]byte, 0, 13+len(blobObj.Hash().Bytes()))
	treeContent = append(treeContent, []byte("100644 f.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, blobObj.Hash().Bytes()...)
	treeObj := makeEncodedObject(t, plumbing.TreeObject, treeContent, format.SHA1)
	_, err = tr.TranslateObject(treeObj)
	require.NoError(t, err)

	// Root commit (no parent).
	commit1Text := "tree " + treeObj.Hash().String() + "\n" +
		"author A <a@b.c> 100 +0000\n" +
		"committer A <a@b.c> 100 +0000\n" +
		"\n" +
		"root\n"
	commit1Obj := makeEncodedObject(t, plumbing.CommitObject, []byte(commit1Text), format.SHA1)
	_, err = tr.TranslateObject(commit1Obj)
	require.NoError(t, err)

	// Child commit with parent.
	commit2Text := "tree " + treeObj.Hash().String() + "\n" +
		"parent " + commit1Obj.Hash().String() + "\n" +
		"author A <a@b.c> 200 +0000\n" +
		"committer A <a@b.c> 200 +0000\n" +
		"\n" +
		"child\n"
	commit2Obj := makeEncodedObject(t, plumbing.CommitObject, []byte(commit2Text), format.SHA1)
	compatHash, err := tr.TranslateObject(commit2Obj)
	require.NoError(t, err)

	// Verify the compat hash was computed and recorded.
	assert.False(t, compatHash.IsZero())
}

func TestTranslateTag(t *testing.T) {
	t.Parallel()

	tr, m := newTestTranslator()

	// Create a blob to tag.
	blobObj := makeEncodedObject(t, plumbing.BlobObject, []byte("tagged content"), format.SHA1)
	_, err := tr.TranslateObject(blobObj)
	require.NoError(t, err)

	tagText := "object " + blobObj.Hash().String() + "\n" +
		"type blob\n" +
		"tag v1.0\n" +
		"tagger Test <t@t.com> 100 +0000\n" +
		"\n" +
		"Release v1.0\n"
	tagObj := makeEncodedObject(t, plumbing.TagObject, []byte(tagText), format.SHA1)
	compatHash, err := tr.TranslateObject(tagObj)
	require.NoError(t, err)

	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 2, count) // blob + tag
	got, err := m.ToCompat(tagObj.Hash())
	require.NoError(t, err)
	assert.True(t, got.Equal(compatHash))
}

func TestTranslateTreeMissingMapping(t *testing.T) {
	t.Parallel()

	tr, _ := newTestTranslator()

	// Build a tree entry with a hash that has no mapping.
	fakeHash := plumbing.NewHash("1111111111111111111111111111111111111111")
	treeContent := make([]byte, 0, 18+len(fakeHash.Bytes()))
	treeContent = append(treeContent, []byte("100644 orphan.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, fakeHash.Bytes()...)

	treeObj := makeEncodedObject(t, plumbing.TreeObject, treeContent, format.SHA1)
	_, err := tr.TranslateObject(treeObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no compat mapping")
}

func TestTranslateCommitMissingTreeMapping(t *testing.T) {
	t.Parallel()

	tr, _ := newTestTranslator()

	fakeTreeHash := plumbing.NewHash("2222222222222222222222222222222222222222")
	commitText := "tree " + fakeTreeHash.String() + "\n" +
		"author A <a@b.c> 100 +0000\n" +
		"committer A <a@b.c> 100 +0000\n" +
		"\n" +
		"test\n"

	commitObj := makeEncodedObject(t, plumbing.CommitObject, []byte(commitText), format.SHA1)
	_, err := tr.TranslateObject(commitObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no compat mapping")
}

func TestReverseTranslateContent(t *testing.T) {
	t.Parallel()

	tr, _ := newTestTranslator()

	blobObj := makeEncodedObject(t, plumbing.BlobObject, []byte("content"), format.SHA1)
	_, err := tr.TranslateObject(blobObj)
	require.NoError(t, err)

	treeContent := make([]byte, 0, 16+len(blobObj.Hash().Bytes()))
	treeContent = append(treeContent, []byte("100644 file.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, blobObj.Hash().Bytes()...)
	treeObj := makeEncodedObject(t, plumbing.TreeObject, treeContent, format.SHA1)
	_, err = tr.TranslateObject(treeObj)
	require.NoError(t, err)

	commitContent := []byte("tree " + treeObj.Hash().String() + "\n" +
		"author Test User <test@example.com> 1234567890 +0000\n" +
		"committer Test User <test@example.com> 1234567890 +0000\n" +
		"\n" +
		"Initial commit\n")
	compatContent, err := tr.ReverseTranslateContent(plumbing.CommitObject, commitContent)
	require.NoError(t, err)

	compatTree, err := tr.Mapping().ToCompat(treeObj.Hash())
	require.NoError(t, err)
	assert.Contains(t, string(compatContent), "tree "+compatTree.String())
	assert.NotContains(t, string(compatContent), treeObj.Hash().String())
}
