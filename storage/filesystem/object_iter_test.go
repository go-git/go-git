package filesystem

import (
	"crypto"
	"errors"
	"io"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

func TestPackfileIter_SeenSkips(t *testing.T) {
	t.Parallel()
	pf := loadPackFixture(t)
	total := len(pf.hashes)

	tests := []struct {
		name      string
		preSeen   func(hashes []plumbing.Hash) []plumbing.Hash
		wantCount int
	}{
		{"no preseed returns all", func(_ []plumbing.Hash) []plumbing.Hash { return nil }, total},
		{"preseed one skips one", func(h []plumbing.Hash) []plumbing.Hash { return h[:1] }, total - 1},
		{"preseed half skips half", func(h []plumbing.Hash) []plumbing.Hash { return h[:total/2] }, total - total/2},
		{"preseed all returns none", func(h []plumbing.Hash) []plumbing.Hash { return h }, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pack := pf.openPack(t)
			seen := make(map[plumbing.Hash]struct{})
			for _, h := range tc.preSeen(pf.hashes) {
				seen[h] = struct{}{}
			}

			iter, err := newPackfileIter(pf.fs, pack, plumbing.AnyObject, seen, pf.idx,
				cache.NewObjectLRUDefault(), false, crypto.SHA1.Size())
			require.NoError(t, err)

			var got int
			require.NoError(t, iter.ForEach(func(plumbing.EncodedObject) error {
				got++
				return nil
			}))
			require.Equal(t, tc.wantCount, got)
		})
	}
}

func TestPackfileIter_SharedSeenDedupsAcrossIterators(t *testing.T) {
	t.Parallel()
	pf := loadPackFixture(t)
	seen := make(map[plumbing.Hash]struct{})
	cch := cache.NewObjectLRUDefault()

	count := func() int {
		pack := pf.openPack(t)
		iter, err := newPackfileIter(pf.fs, pack, plumbing.AnyObject, seen, pf.idx,
			cch, false, crypto.SHA1.Size())
		require.NoError(t, err)

		var n int
		require.NoError(t, iter.ForEach(func(plumbing.EncodedObject) error {
			n++
			return nil
		}))
		return n
	}

	first := count()
	second := count()

	require.Equal(t, len(pf.hashes), first)
	require.Zero(t, second, "shared seen map should suppress duplicates from second iteration")
	require.Len(t, seen, len(pf.hashes))
}

func TestPackfileIter_ForEachClosesPack(t *testing.T) {
	t.Parallel()
	pf := loadPackFixture(t)
	stopAt := len(pf.hashes) / 2
	cbErr := errors.New("stop")

	tests := []struct {
		name    string
		cb      func(int) error
		wantErr error
	}{
		{
			name:    "callback returns nil completes",
			cb:      func(int) error { return nil },
			wantErr: nil,
		},
		{
			name:    "callback returns error aborts",
			cb:      func(_ int) error { return cbErr },
			wantErr: cbErr,
		},
		{
			name: "callback errors midway",
			cb: func(i int) error {
				if i >= stopAt {
					return cbErr
				}
				return nil
			},
			wantErr: cbErr,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pack := &closeCounter{File: pf.openPack(t)}
			iter, err := newPackfileIter(pf.fs, pack, plumbing.AnyObject,
				make(map[plumbing.Hash]struct{}), pf.idx,
				cache.NewObjectLRUDefault(), false, crypto.SHA1.Size())
			require.NoError(t, err)

			var i int
			err = iter.ForEach(func(plumbing.EncodedObject) error {
				defer func() { i++ }()
				return tc.cb(i)
			})
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
			require.Equal(t, 1, pack.closed, "pack must be closed after ForEach returns")
		})
	}
}

func TestPackfileIter_ForEachKeepsPackOpen(t *testing.T) {
	t.Parallel()
	pf := loadPackFixture(t)
	pack := &closeCounter{File: pf.openPack(t)}

	iter, err := newPackfileIter(pf.fs, pack, plumbing.AnyObject,
		make(map[plumbing.Hash]struct{}), pf.idx,
		cache.NewObjectLRUDefault(), true, crypto.SHA1.Size())
	require.NoError(t, err)

	require.NoError(t, iter.ForEach(func(plumbing.EncodedObject) error { return nil }))
	require.Zero(t, pack.closed, "keepPack=true must not close the underlying file")
}

func TestNewPackfileIter_ClosesPackOnGetByTypeError(t *testing.T) {
	t.Parallel()
	pf := loadPackFixture(t)

	tests := []struct {
		name        string
		keepPack    bool
		wantClosed  int
		wantClosedR string
	}{
		{"keepPack false closes on error", false, 1, "must close when keepPack is false"},
		{"keepPack true leaves it open", true, 0, "must not close when keepPack is true"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pack := &closeCounter{File: pf.openPack(t)}
			iter, err := newPackfileIter(pf.fs, pack, plumbing.OFSDeltaObject,
				make(map[plumbing.Hash]struct{}), pf.idx,
				cache.NewObjectLRUDefault(), tc.keepPack, crypto.SHA1.Size())
			require.Error(t, err)
			require.Nil(t, iter)
			require.Equal(t, tc.wantClosed, pack.closed, tc.wantClosedR)
		})
	}
}

func TestObjectsIter_ForEachClosesOnError(t *testing.T) {
	t.Parallel()
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	require.NoError(t, err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	cbErr := errors.New("stop")

	tests := []struct {
		name    string
		cb      func(plumbing.EncodedObject) error
		wantErr error
	}{
		{"cb returns nil completes", func(plumbing.EncodedObject) error { return nil }, nil},
		{"cb returns error propagates", func(plumbing.EncodedObject) error { return cbErr }, cbErr},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objects, err := o.dir.Objects()
			require.NoError(t, err)
			require.NotEmpty(t, objects)

			iter := &objectsIter{s: o, t: plumbing.AnyObject, h: append([]plumbing.Hash{}, objects...)}
			err = iter.ForEach(tc.cb)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
			require.Empty(t, iter.h, "iter.h must be drained by Close after ForEach returns")
		})
	}
}

type packFixture struct {
	fs       billy.Filesystem
	dg       *dotgit.DotGit
	packHash plumbing.Hash
	idx      idxfile.Index
	hashes   []plumbing.Hash
}

func loadPackFixture(t *testing.T) packFixture {
	t.Helper()

	fs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	require.NoError(t, err)
	dg := dotgit.New(fs)

	packs, err := dg.ObjectPacks()
	require.NoError(t, err)
	require.NotEmpty(t, packs)

	idxFile, err := dg.ObjectPackIdx(packs[0])
	require.NoError(t, err)
	t.Cleanup(func() { _ = idxFile.Close() })

	idx := idxfile.NewMemoryIndex(crypto.SHA1.Size())
	require.NoError(t, idxfile.NewDecoder(idxFile, hash.New(crypto.SHA1)).Decode(idx))

	entries, err := idx.Entries()
	require.NoError(t, err)
	t.Cleanup(func() { _ = entries.Close() })

	var hashes []plumbing.Hash
	for {
		e, err := entries.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		hashes = append(hashes, e.Hash)
	}

	return packFixture{fs: fs, dg: dg, packHash: packs[0], idx: idx, hashes: hashes}
}

func (p packFixture) openPack(t *testing.T) billy.File {
	t.Helper()
	f, err := p.dg.ObjectPack(p.packHash)
	require.NoError(t, err)
	return f
}

type closeCounter struct {
	billy.File
	closed int
}

func (c *closeCounter) Close() error {
	c.closed++
	return c.File.Close()
}
