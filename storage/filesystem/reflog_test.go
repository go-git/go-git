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
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestReflogReadNonExistent(t *testing.T) {
	t.Parallel()
	sto := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	entries, err := sto.Reflog(plumbing.ReferenceName("refs/heads/no-such-ref"))
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestReflogAppendAndRead(t *testing.T) {
	t.Parallel()
	sto := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()
	ref := plumbing.ReferenceName("refs/heads/main")

	e1 := &reflog.Entry{
		OldHash: plumbing.ZeroHash,
		NewHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Committer: reflog.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Unix(1000000000, 0).UTC(),
		},
		Message: "commit (initial): first",
	}
	e2 := &reflog.Entry{
		OldHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		NewHash: plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		Committer: reflog.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Unix(1000000001, 0).UTC(),
		},
		Message: "commit: second",
	}

	require.NoError(t, sto.AppendReflog(ref, e1))
	require.NoError(t, sto.AppendReflog(ref, e2))

	entries, err := sto.Reflog(ref)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, "commit (initial): first", entries[0].Message)
	assert.Equal(t, "commit: second", entries[1].Message)
	assert.Equal(t, plumbing.ZeroHash, entries[0].OldHash)
	assert.Equal(t, e1.NewHash, entries[1].OldHash)
}

func TestReflogDelete(t *testing.T) {
	t.Parallel()
	sto := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()
	ref := plumbing.ReferenceName("refs/heads/main")

	require.NoError(t, sto.AppendReflog(ref, &reflog.Entry{
		OldHash: plumbing.ZeroHash,
		NewHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Committer: reflog.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Unix(1000000000, 0).UTC(),
		},
		Message: "commit (initial): first",
	}))

	require.NoError(t, sto.DeleteReflog(ref))

	entries, err := sto.Reflog(ref)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestReflogDeleteNonExistent(t *testing.T) {
	t.Parallel()
	sto := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	err := sto.DeleteReflog(plumbing.ReferenceName("refs/heads/no-such-ref"))
	assert.NoError(t, err)
}
