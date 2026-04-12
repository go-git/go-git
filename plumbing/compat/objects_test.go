package compat_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestTranslateStoredObjects(t *testing.T) {
	t.Parallel()

	// Create a memory storage with SHA-1 objects.
	s := memory.NewStorage(memory.WithObjectFormat(format.SHA1))

	oh := plumbing.FromObjectFormat(format.SHA1)

	// Store a blob.
	blobContent := []byte("hello world\n")
	blob := plumbing.NewMemoryObject(oh)
	blob.SetType(plumbing.BlobObject)
	blob.Write(blobContent)
	blob.SetSize(int64(len(blobContent)))
	blobHash, err := s.ObjectStorage.SetEncodedObject(blob)
	require.NoError(t, err)

	// Store a tree referencing the blob.
	treeContent := make([]byte, 0, 17+len(blobHash.Bytes()))
	treeContent = append(treeContent, []byte("100644 hello.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, blobHash.Bytes()...)
	tree := plumbing.NewMemoryObject(oh)
	tree.SetType(plumbing.TreeObject)
	tree.Write(treeContent)
	tree.SetSize(int64(len(treeContent)))
	treeHash, err := s.ObjectStorage.SetEncodedObject(tree)
	require.NoError(t, err)

	// Store a commit referencing the tree.
	commitText := "tree " + treeHash.String() + "\n" +
		"author Test <t@t.com> 100 +0000\n" +
		"committer Test <t@t.com> 100 +0000\n" +
		"\n" +
		"test commit\n"
	commit := plumbing.NewMemoryObject(oh)
	commit.SetType(plumbing.CommitObject)
	commit.Write([]byte(commitText))
	commit.SetSize(int64(len(commitText)))
	commitHash, err := s.ObjectStorage.SetEncodedObject(commit)
	require.NoError(t, err)

	// Store a tag referencing the commit.
	tagText := "object " + commitHash.String() + "\n" +
		"type commit\n" +
		"tag v1.0\n" +
		"tagger Test <t@t.com> 100 +0000\n" +
		"\n" +
		"release\n"
	tag := plumbing.NewMemoryObject(oh)
	tag.SetType(plumbing.TagObject)
	tag.Write([]byte(tagText))
	tag.SetSize(int64(len(tagText)))
	tagHash, err := s.ObjectStorage.SetEncodedObject(tag)
	require.NoError(t, err)

	// Create a translator from SHA-1 (native) to SHA-256 (compat).
	m := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, m)

	// Translate all stored objects.
	err = compat.TranslateStoredObjects(s, tr)
	require.NoError(t, err)

	// Verify all 4 objects have mappings.
	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 4, count)

	// Verify each object's mapping exists.
	for _, h := range []plumbing.Hash{blobHash, treeHash, commitHash, tagHash} {
		compatHash, err := m.NativeToCompat(h)
		require.NoError(t, err, "missing mapping for %s", h)
		assert.False(t, compatHash.IsZero())
	}
}

func TestTranslateStoredObjectsEmpty(t *testing.T) {
	t.Parallel()

	s := memory.NewStorage()
	m := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, m)

	err := compat.TranslateStoredObjects(s, tr)
	require.NoError(t, err)
	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestTranslateStoredObjectsReportsUnresolvableDependencies(t *testing.T) {
	t.Parallel()

	s := memory.NewStorage(memory.WithObjectFormat(format.SHA1))
	oh := plumbing.FromObjectFormat(format.SHA1)

	treeContent := make([]byte, 0, 19+len(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes()))
	treeContent = append(treeContent, []byte("100644 missing.txt")...)
	treeContent = append(treeContent, 0x00)
	treeContent = append(treeContent, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes()...)

	tree := plumbing.NewMemoryObject(oh)
	tree.SetType(plumbing.TreeObject)
	tree.Write(treeContent)
	tree.SetSize(int64(len(treeContent)))
	_, err := s.ObjectStorage.SetEncodedObject(tree)
	require.NoError(t, err)

	m := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, m)

	err = compat.TranslateStoredObjects(s, tr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependencies")
}

func TestTranslateStoredObjectsSurfacesNonDependencyErrors(t *testing.T) {
	t.Parallel()

	s := memory.NewStorage(memory.WithObjectFormat(format.SHA1))
	oh := plumbing.FromObjectFormat(format.SHA1)

	treeContent := []byte("100644 broken.txt")
	tree := plumbing.NewMemoryObject(oh)
	tree.SetType(plumbing.TreeObject)
	tree.Write(treeContent)
	tree.SetSize(int64(len(treeContent)))
	_, err := s.ObjectStorage.SetEncodedObject(tree)
	require.NoError(t, err)

	m := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, m)

	err = compat.TranslateStoredObjects(s, tr)
	require.Error(t, err)
	assert.ErrorContains(t, err, "malformed tree entry")
	assert.NotContains(t, err.Error(), "missing dependencies")
}

func TestTranslateStoredObjectsTranslatesTagOfTag(t *testing.T) {
	t.Parallel()

	s := memory.NewStorage(memory.WithObjectFormat(format.SHA1))
	oh := plumbing.FromObjectFormat(format.SHA1)

	blobContent := []byte("hello world\n")
	blob := plumbing.NewMemoryObject(oh)
	blob.SetType(plumbing.BlobObject)
	blob.Write(blobContent)
	blob.SetSize(int64(len(blobContent)))
	blobHash, err := s.ObjectStorage.SetEncodedObject(blob)
	require.NoError(t, err)

	tag1Text := "object " + blobHash.String() + "\n" +
		"type blob\n" +
		"tag v1.0\n" +
		"tagger Test <t@t.com> 100 +0000\n" +
		"\n" +
		"release\n"
	tag1 := plumbing.NewMemoryObject(oh)
	tag1.SetType(plumbing.TagObject)
	tag1.Write([]byte(tag1Text))
	tag1.SetSize(int64(len(tag1Text)))
	tag1Hash := tag1.Hash()

	tag2Text := "object " + tag1Hash.String() + "\n" +
		"type tag\n" +
		"tag v1.1\n" +
		"tagger Test <t@t.com> 100 +0000\n" +
		"\n" +
		"release candidate\n"
	tag2 := plumbing.NewMemoryObject(oh)
	tag2.SetType(plumbing.TagObject)
	tag2.Write([]byte(tag2Text))
	tag2.SetSize(int64(len(tag2Text)))
	tag2Hash := tag2.Hash()

	_, err = s.ObjectStorage.SetEncodedObject(tag2)
	require.NoError(t, err)
	_, err = s.ObjectStorage.SetEncodedObject(tag1)
	require.NoError(t, err)

	m := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, m)

	ordered := orderedTagStorer{
		EncodedObjectStorer: s,
		tagOrder:            []plumbing.Hash{tag2Hash, tag1Hash},
	}
	require.NoError(t, compat.TranslateStoredObjects(ordered, tr))

	compatTag1, err := m.NativeToCompat(tag1Hash)
	require.NoError(t, err)
	compatTag2, err := m.NativeToCompat(tag2Hash)
	require.NoError(t, err)
	assert.False(t, compatTag1.IsZero())
	assert.False(t, compatTag2.IsZero())
}

type orderedTagStorer struct {
	storer.EncodedObjectStorer
	tagOrder []plumbing.Hash
}

func (s orderedTagStorer) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	if t != plumbing.TagObject {
		return s.EncodedObjectStorer.IterEncodedObjects(t)
	}

	series := make([]plumbing.EncodedObject, 0, len(s.tagOrder))
	for _, h := range s.tagOrder {
		obj, err := s.EncodedObject(plumbing.TagObject, h)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				continue
			}
			return nil, err
		}
		series = append(series, obj)
	}

	return storer.NewEncodedObjectSliceIter(series), nil
}
