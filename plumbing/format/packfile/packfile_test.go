package packfile_test

import (
	"crypto"
	"io"
	"math"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/fixtureutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func TestGet(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.Entries(f)
		p := newPackfile(t, f)

		for h := range entries {
			obj, err := p.Get(h)
			require.NoError(t, err)
			require.NotNil(t, obj)
			assert.Equal(t, h.String(), obj.Hash().String())
		}

		_, err := p.Get(plumbing.ZeroHash)
		assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

		id, err := p.ID()
		require.NoError(t, err)
		assert.Equal(t, f.PackfileHash, id.String())
	})
}

func TestGetByOffset(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.Entries(f)
		p := newPackfile(t, f)

		for h, o := range entries {
			obj, err := p.GetByOffset(o)
			require.NoError(t, err)
			require.NotNil(t, obj)
			assert.Equal(t, h.String(), obj.Hash().String())
		}

		_, err := p.GetByOffset(math.MaxInt64)
		assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
	})
}

func TestGetAll(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.Entries(f)
		p := newPackfile(t, f)

		iter, err := p.GetAll()
		require.NoError(t, err)

		var objects int
		for {
			o, err := iter.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			require.NotNil(t, o)

			objects++
			h := o.Hash()
			_, ok := entries[h]
			assert.True(t, ok, "%s not found", h)
		}

		assert.Len(t, entries, objects)

		iter.Close()
		require.NoError(t, p.Close())
	})
}

func TestDecode(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.Entries(f)
		p := newPackfile(t, f)

		for h := range entries {
			obj, err := p.Get(h)
			require.NoError(t, err)
			assert.Equal(t, h.String(), obj.Hash().String())
		}

		require.NoError(t, p.Close())
	})
}

func TestDecodeByTypeRefDelta(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile").ByTag("ref-delta")
	require.GreaterOrEqual(t, len(packs), 1)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		types := []plumbing.ObjectType{
			plumbing.CommitObject,
			plumbing.TagObject,
			plumbing.TreeObject,
			plumbing.BlobObject,
		}

		var total int
		for _, typ := range types {
			p := newPackfile(t, f)

			iter, err := p.GetByType(typ)
			require.NoError(t, err)

			err = iter.ForEach(func(obj plumbing.EncodedObject) error {
				assert.Equal(t, typ, obj.Type())
				total++
				return nil
			})
			require.NoError(t, err)
			require.NoError(t, p.Close())
		}

		assert.Equal(t, int(f.ObjectsCount), total)
	})
}

func TestDecodeByType(t *testing.T) {
	t.Parallel()

	types := []plumbing.ObjectType{
		plumbing.CommitObject,
		plumbing.TagObject,
		plumbing.TreeObject,
		plumbing.BlobObject,
	}

	packs := fixtures.ByTag("packfile")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		for _, typ := range types {
			p := newPackfile(t, f)

			iter, err := p.GetByType(typ)
			require.NoError(t, err)

			err = iter.ForEach(func(obj plumbing.EncodedObject) error {
				assert.Equal(t, typ, obj.Type())
				return nil
			})
			require.NoError(t, err)

			require.NoError(t, p.Close())
		}
	})
}

func TestDecodeByTypeConstructor(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		p := newPackfile(t, f)
		defer p.Close()

		_, err := p.GetByType(plumbing.OFSDeltaObject)
		assert.ErrorIs(t, err, plumbing.ErrInvalidType)

		_, err = p.GetByType(plumbing.REFDeltaObject)
		assert.ErrorIs(t, err, plumbing.ErrInvalidType)

		_, err = p.GetByType(plumbing.InvalidObject)
		assert.ErrorIs(t, err, plumbing.ErrInvalidType)
	})
}

func TestSize(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := f.Entries()
		p := newPackfile(t, f)
		defer p.Close()

		for h, offset := range entries {
			size, err := p.GetSizeByOffset(offset)
			require.NoError(t, err, "object %s", h)
			assert.GreaterOrEqual(t, size, int64(0), "object %s", h)
		}

		if f.Head != "" {
			offset, err := p.FindOffset(plumbing.NewHash(f.Head))
			require.NoError(t, err)
			size, err := p.GetSizeByOffset(offset)
			require.NoError(t, err)
			assert.Greater(t, size, int64(0))
		}
	})
}

func objectFormatHash(format string) crypto.Hash {
	if format == "sha256" {
		return crypto.SHA256
	}
	return crypto.SHA1
}

func getIndexFromFixture(t testing.TB, f *fixtures.Fixture) idxfile.Index {
	t.Helper()

	idxFile, err := f.Idx()
	require.NoError(t, err)
	defer idxFile.Close()

	h := objectFormatHash(f.ObjectFormat)
	idx := idxfile.NewMemoryIndex(h.Size())
	require.NoError(t, idxfile.NewDecoder(idxFile, hash.New(h)).Decode(idx))
	return idx
}

func newPackfile(t testing.TB, f *fixtures.Fixture) *packfile.Packfile {
	t.Helper()

	index := getIndexFromFixture(t, f)
	pf, err := f.Packfile()
	require.NoError(t, err)

	opts := []packfile.PackfileOption{
		packfile.WithIdx(index),
		packfile.WithFs(osfs.New(t.TempDir())),
	}
	if f.ObjectFormat == "sha256" {
		opts = append(opts, packfile.WithObjectIDSize(config.SHA256.Size()))
	}

	return packfile.NewPackfile(pf, opts...)
}

func BenchmarkGetByOffset(b *testing.B) {
	for _, format := range []string{"sha1", "sha256"} {
		packs := fixtures.ByTag("packfile-entries").ByObjectFormat(format)
		if len(packs) == 0 {
			continue
		}
		f := packs.One()

		idx := getIndexFromFixture(b, f)
		entries := fixtureutil.Entries(f)
		c := cache.NewObjectLRUDefault()

		b.Run(format+"/with_storage",
			func() func(b *testing.B) {
				pf1, err := f.Packfile()
				if err != nil {
					b.Fatal(err)
				}
				opts := []packfile.PackfileOption{
					packfile.WithIdx(idx),
					packfile.WithFs(osfs.New(b.TempDir())),
					packfile.WithCache(c),
				}
				if f.ObjectFormat == "sha256" {
					opts = append(opts, packfile.WithObjectIDSize(config.SHA256.Size()))
				}
				return benchmarkGetByOffset(entries, packfile.NewPackfile(pf1, opts...))
			}())
		b.Run(format+"/without_storage",
			func() func(b *testing.B) {
				pf2, err := f.Packfile()
				if err != nil {
					b.Fatal(err)
				}
				opts := []packfile.PackfileOption{
					packfile.WithCache(c),
					packfile.WithIdx(idx),
				}
				if f.ObjectFormat == "sha256" {
					opts = append(opts, packfile.WithObjectIDSize(config.SHA256.Size()))
				}
				return benchmarkGetByOffset(entries, packfile.NewPackfile(pf2, opts...))
			}())
	}
}

func benchmarkGetByOffset(entries map[plumbing.Hash]int64, p *packfile.Packfile) func(b *testing.B) {
	return func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for h, o := range entries {
				obj, err := p.GetByOffset(o)
				if err != nil {
					b.Fatal()
				}
				if h != obj.Hash() {
					b.Fatal()
				}
			}
		}
	}
}
