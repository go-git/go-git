package packfile_test

import (
	"io"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/fixtureutil"
	"github.com/go-git/go-git/v6/plumbing"
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

	// Read all objects concurrently. Before the fix, this triggered a data
	// race on the shared Seek cursor inside the pack file handle.
	const iterations = 5
	var wg sync.WaitGroup
	errCh := make(chan error, len(objects)*iterations)

	for range iterations {
		for _, obj := range objects {
			wg.Go(func() {
				reader, err := obj.Reader()
				if err != nil {
					errCh <- err
					return
				}
				defer reader.Close()

				_, err = io.ReadAll(reader)
				errCh <- err
			})
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}
