package filesystem_test

import (
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestReftableStorage(t *testing.T) {
	t.Parallel()
	fs := memfs.New()

	// Create "reftable" directory to trigger reftable backend activation.
	err := fs.MkdirAll("reftable", 0o755)
	require.NoError(t, err)

	// Write config enabling reftable extension.
	configContent := `[core]
	repositoryformatversion = 1
[extensions]
	refStorage = reftable
`
	f, err := fs.Create("config")
	require.NoError(t, err)
	_, err = f.Write([]byte(configContent))
	require.NoError(t, err)
	_ = f.Close()

	sto := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{})
	defer func() { _ = sto.Close() }()

	// Test SetReference and Reference
	refName := plumbing.ReferenceName("refs/heads/main")
	refHash := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")
	ref := plumbing.NewHashReference(refName, refHash)

	err = sto.SetReference(ref)
	require.NoError(t, err)

	gotRef, err := sto.Reference(refName)
	require.NoError(t, err)
	assert.Equal(t, refHash, gotRef.Hash())

	// Test CheckAndSetReference (Match expected)
	newHash := plumbing.NewHash("abcdef0123456789abcdef0123456789abcdef01")
	newRef := plumbing.NewHashReference(refName, newHash)
	err = sto.CheckAndSetReference(newRef, ref)
	require.NoError(t, err)

	gotRef, err = sto.Reference(refName)
	require.NoError(t, err)
	assert.Equal(t, newHash, gotRef.Hash())

	// Test CheckAndSetReference (Mismatch expected)
	err = sto.CheckAndSetReference(ref, ref) // expected was old 'ref', current is 'newRef'
	assert.ErrorIs(t, err, storage.ErrReferenceHasChanged)

	// Test IterReferences
	it, err := sto.IterReferences()
	require.NoError(t, err)
	var refNames []string
	err = it.ForEach(func(r *plumbing.Reference) error {
		refNames = append(refNames, r.Name().String())
		return nil
	})
	require.NoError(t, err)
	assert.Contains(t, refNames, "refs/heads/main")

	// Test RemoveReference
	err = sto.RemoveReference(refName)
	require.NoError(t, err)

	_, err = sto.Reference(refName)
	assert.Equal(t, plumbing.ErrReferenceNotFound, err)

	// Test Reflog
	committer := reflog.Signature{
		Name:  "Test Committer",
		Email: "test@example.com",
		When:  time.Now().Truncate(time.Second),
	}
	entry := &reflog.Entry{
		OldHash:   plumbing.ZeroHash,
		NewHash:   refHash,
		Committer: committer,
		Message:   "test reflog message",
	}

	err = sto.AppendReflog(refName, entry)
	require.NoError(t, err)

	entries, err := sto.Reflog(refName)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, entry.Message, entries[0].Message)
	assert.Equal(t, entry.Committer.Name, entries[0].Committer.Name)

	// Test DeleteReflog
	err = sto.DeleteReflog(refName)
	require.NoError(t, err)

	entries, err = sto.Reflog(refName)
	require.NoError(t, err)
	assert.Len(t, entries, 0)
}

func TestReftableStorageBrokenFallback(t *testing.T) {
	t.Parallel()
	fs := memfs.New()

	// Write config enabling reftable extension.
	configContent := `[core]
	repositoryformatversion = 1
[extensions]
	refStorage = reftable
`
	f, err := fs.Create("config")
	require.NoError(t, err)
	_, err = f.Write([]byte(configContent))
	require.NoError(t, err)
	_ = f.Close()

	// Create a corrupted reftable directory.
	err = fs.MkdirAll("reftable", 0o755)
	require.NoError(t, err)

	// Create a tables.list referencing a nonexistent file.
	tl, err := fs.Create("reftable/tables.list")
	require.NoError(t, err)
	_, err = tl.Write([]byte("nonexistent.ref\n"))
	require.NoError(t, err)
	_ = tl.Close()

	sto := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{})
	defer func() { _ = sto.Close() }()

	// Every reference and reflog operation should return an error, and NOT fall back to loose refs.
	refName := plumbing.ReferenceName("refs/heads/main")
	refHash := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")
	ref := plumbing.NewHashReference(refName, refHash)

	err = sto.SetReference(ref)
	assert.Error(t, err)

	_, err = sto.Reference(refName)
	assert.Error(t, err)

	_, err = sto.IterReferences()
	assert.Error(t, err)

	err = sto.RemoveReference(refName)
	assert.Error(t, err)

	_, err = sto.Reflog(refName)
	assert.Error(t, err)

	err = sto.AppendReflog(refName, &reflog.Entry{})
	assert.Error(t, err)
}
