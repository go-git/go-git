package packfile_test

import (
	"io"
	"testing"
	"testing/synctest"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/fixtureutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

// TestFSObjectConcurrentReader verifies that multiple goroutines can call
// Reader() on FSObjects backed by the same packfile handle without racing.
// Before the ReadAt fix, FSObject.Reader() used Seek+Read on a shared file
// descriptor, causing data races under concurrent access.
func TestFSObjectConcurrentReader(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	index := getIndexFromFixture(t, f)

	pf, pfErr := f.Packfile()
	require.NoError(t, pfErr)
	p := packfile.NewPackfile(pf,
		packfile.WithIdx(index),
		packfile.WithFs(osfs.New(t.TempDir())),
	)

	entries := fixtureutil.Entries(f)
	objects := make([]plumbing.EncodedObject, 0, len(entries))
	for h := range entries {
		obj, err := p.Get(h)
		require.NoError(t, err)
		objects = append(objects, obj)
	}
	require.NotEmpty(t, objects)

	const iterations = 5
	synctest.Test(t, func(t *testing.T) {
		for range iterations {
			for _, obj := range objects {
				go func() {
					reader, err := obj.Reader()
					if err != nil {
						t.Errorf("Reader(): %v", err)
						return
					}
					defer reader.Close()

					if _, err = io.ReadAll(reader); err != nil {
						t.Errorf("ReadAll(): %v", err)
					}
				}()
			}
		}
	})
}

// TestFSObjectReaderAfterFailedReopen verifies that Reader returns an
// error, and does not panic, on each call when the pack handle is
// closed and the pack cannot be opened again. An earlier Reader kept
// the nil handle from the failed open, so the next call panicked.
// See https://github.com/go-git/go-git/issues/1787.
func TestFSObjectReaderAfterFailedReopen(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	closedPack, err := fs.Create("closed.pack")
	require.NoError(t, err)
	require.NoError(t, closedPack.Close())

	obj := packfile.NewFSObject(
		plumbing.ZeroHash,
		plumbing.BlobObject,
		0,
		1,
		nil,
		fs,
		closedPack,
		"missing.pack",
		cache.NewObjectLRUDefault(),
	)

	for range 2 {
		_, err := obj.Reader()
		require.Error(t, err)
	}
}
