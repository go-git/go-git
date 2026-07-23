package git

import (
	"context"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// TestContextCancellationPropagates asserts that a canceled context reaches
// the storer through the full read path: Repository.Log resolution and the
// commit iterator both observe cancellation instead of silently swapping in
// a background context.
func TestContextCancellationPropagates(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	dotgit, err := f.DotGit()
	require.NoError(t, err)
	st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	r, err := Open(t.Context(), st, memfs.New())
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	// Entry-point resolution observes cancellation.
	_, err = r.Log(canceled, &LogOptions{})
	require.ErrorIs(t, err, context.Canceled)

	// Iteration observes cancellation: obtain a live iterator with a good
	// ctx, then cancel mid-iteration.
	iter, err := r.Log(t.Context(), &LogOptions{})
	require.NoError(t, err)
	defer iter.Close()

	err = iter.ForEach(canceled, func(*object.Commit) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
}
