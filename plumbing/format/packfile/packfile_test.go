package packfile_test

import (
	"crypto"
	"io"
	"math"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	idx := getIndexFromIdxFile(f.Idx())

	p := packfile.NewPackfile(f.Packfile(),
		packfile.WithIdx(idx), packfile.WithFs(fixtures.Filesystem),
	)

	for h := range expectedEntries {
		obj, err := p.Get(h)

		assert.NoError(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, h.String(), obj.Hash().String())
	}

	_, err := p.Get(plumbing.ZeroHash)
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	id, err := p.ID()
	assert.NoError(t, err)
	assert.Equal(t, f.PackfileHash, id.String())
}

func TestGetByOffset(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	idx := getIndexFromIdxFile(f.Idx())

	p := packfile.NewPackfile(f.Packfile(),
		packfile.WithIdx(idx), packfile.WithFs(fixtures.Filesystem),
	)

	for h, o := range expectedEntries {
		obj, err := p.GetByOffset(o)
		assert.NoError(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, h.String(), obj.Hash().String())
	}

	_, err := p.GetByOffset(math.MaxInt64)
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}

func TestGetAll(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()
	idx := getIndexFromIdxFile(f.Idx())

	p := packfile.NewPackfile(f.Packfile(),
		packfile.WithIdx(idx),
		packfile.WithFs(fixtures.Filesystem))

	iter, err := p.GetAll()
	assert.NoError(t, err)

	var objects int
	for {
		o, err := iter.Next()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)

		objects++
		h := o.Hash()
		_, ok := expectedEntries[h]
		assert.True(t, ok, "%s not found", h)
	}

	assert.Len(t, expectedEntries, objects)

	iter.Close()
	assert.NoError(t, p.Close())
}

func TestDecode(t *testing.T) {
	t.Parallel()

	packfiles := fixtures.Basic().ByTag("packfile")
	assert.Greater(t, len(packfiles), 0)

	for _, f := range packfiles {
		f := f
		index := getIndexFromIdxFile(f.Idx())

		p := packfile.NewPackfile(f.Packfile(),
			packfile.WithIdx(index), packfile.WithFs(fixtures.Filesystem),
		)

		for _, h := range expectedHashes {
			h := h
			obj, err := p.Get(plumbing.NewHash(h))
			assert.NoError(t, err)
			assert.Equal(t, obj.Hash().String(), h)
		}

		err := p.Close()
		assert.NoError(t, err)
	}
}

func TestDecodeByTypeRefDelta(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().ByTag("ref-delta").One()

	index := getIndexFromIdxFile(f.Idx())

	packfile := packfile.NewPackfile(f.Packfile(),
		packfile.WithIdx(index), packfile.WithFs(fixtures.Filesystem))

	iter, err := packfile.GetByType(plumbing.CommitObject)
	assert.NoError(t, err)

	var count int
	for {
		obj, err := iter.Next()
		if err == io.EOF {
			break
		}

		count++
		assert.NoError(t, err)
		assert.Equal(t, obj.Type(), plumbing.CommitObject)
	}

	err = packfile.Close()

	assert.NoError(t, err)
	assert.Greater(t, count, 0)
}

func TestDecodeByType(t *testing.T) {
	t.Parallel()

	types := []plumbing.ObjectType{
		plumbing.CommitObject,
		plumbing.TagObject,
		plumbing.TreeObject,
		plumbing.BlobObject,
	}

	for _, f := range fixtures.Basic().ByTag("packfile") {
		f := f
		for _, typ := range types {
			typ := typ
			index := getIndexFromIdxFile(f.Idx())

			packfile := packfile.NewPackfile(f.Packfile(),
				packfile.WithIdx(index), packfile.WithFs(fixtures.Filesystem),
			)
			defer packfile.Close()

			iter, err := packfile.GetByType(typ)
			assert.NoError(t, err)

			err = iter.ForEach(func(obj plumbing.EncodedObject) error {
				assert.Equal(t, typ, obj.Type())
				return nil
			})
			assert.NoError(t, err)
		}
	}
}

func TestDecodeByTypeConstructor(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().ByTag("packfile").One()
	index := getIndexFromIdxFile(f.Idx())

	packfile := packfile.NewPackfile(f.Packfile(),
		packfile.WithIdx(index), packfile.WithFs(fixtures.Filesystem),
	)
	defer packfile.Close()

	_, err := packfile.GetByType(plumbing.OFSDeltaObject)
	assert.ErrorIs(t, err, plumbing.ErrInvalidType)

	_, err = packfile.GetByType(plumbing.REFDeltaObject)
	assert.ErrorIs(t, err, plumbing.ErrInvalidType)

	_, err = packfile.GetByType(plumbing.InvalidObject)
	assert.ErrorIs(t, err, plumbing.ErrInvalidType)
}

func getIndexFromIdxFile(r io.ReadCloser) idxfile.Index {
	defer r.Close()

	idx := idxfile.NewMemoryIndex(crypto.SHA1.Size())
	if err := idxfile.NewDecoder(r).Decode(idx); err != nil {
		panic(err)
	}

	return idx
}

func TestSize(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().ByTag("ref-delta").One()

	index := getIndexFromIdxFile(f.Idx())

	packfile := packfile.NewPackfile(f.Packfile(),
		packfile.WithIdx(index),
		packfile.WithFs(fixtures.Filesystem),
	)
	defer packfile.Close()

	// Get the size of binary.jpg, which is not delta-encoded.
	offset, err := packfile.FindOffset(plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d"))
	assert.NoError(t, err)

	size, err := packfile.GetSizeByOffset(offset)
	assert.NoError(t, err)
	assert.Equal(t, int64(76110), size)

	// Get the size of the root commit, which is delta-encoded.
	offset, err = packfile.FindOffset(plumbing.NewHash(f.Head))
	assert.NoError(t, err)
	size, err = packfile.GetSizeByOffset(offset)
	assert.NoError(t, err)
	assert.Equal(t, int64(245), size)
}

func BenchmarkGetByOffset(b *testing.B) {
	f := fixtures.Basic().One()
	idx := idxfile.NewMemoryIndex(crypto.SHA1.Size())

	cache := cache.NewObjectLRUDefault()
	err := idxfile.NewDecoder(f.Idx()).Decode(idx)
	require.NoError(b, err)

	b.Run("with storage",
		benchmarkGetByOffset(packfile.NewPackfile(f.Packfile(),
			packfile.WithIdx(idx), packfile.WithFs(fixtures.Filesystem),
			packfile.WithCache(cache),
		)))
	b.Run("without storage",
		benchmarkGetByOffset(packfile.NewPackfile(f.Packfile(),
			packfile.WithCache(cache), packfile.WithIdx(idx),
		)))
}

func benchmarkGetByOffset(p *packfile.Packfile) func(b *testing.B) {
	return func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for h, o := range expectedEntries {
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

var expectedHashes = []string{
	"918c48b83bd081e863dbe1b80f8998f058cd8294",
	"af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
	"1669dce138d9b841a518c64b10914d88f5e488ea",
	"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
	"b8e471f58bcbca63b07bda20e428190409c2db47",
	"35e85108805c84807bc66a02d91535e1e24b38b9",
	"b029517f6300c2da0f4b651b8642506cd6aaf45d",
	"32858aad3c383ed1ff0a0f9bdf231d54a00c9e88",
	"d3ff53e0564a9f87d8e84b6e28e5060e517008aa",
	"c192bd6a24ea1ab01d78686e417c8bdc7c3d197f",
	"d5c0f4ab811897cadf03aec358ae60d21f91c50d",
	"49c6bb89b17060d7b4deacb7b338fcc6ea2352a9",
	"cf4aa3b38974fb7d81f367c0830f7d78d65ab86b",
	"9dea2395f5403188298c1dabe8bdafe562c491e3",
	"586af567d0bb5e771e49bdd9434f5e0fb76d25fa",
	"9a48f23120e880dfbe41f7c9b7b708e9ee62a492",
	"5a877e6a906a2743ad6e45d99c1793642aaf8eda",
	"c8f1d8c61f9da76f4cb49fd86322b6e685dba956",
	"a8d315b2b1c615d43042c3a62402b8a54288cf5c",
	"a39771a7651f97faf5c72e08224d857fc35133db",
	"880cd14280f4b9b6ed3986d6671f907d7cc2a198",
	"fb72698cab7617ac416264415f13224dfd7a165e",
	"4d081c50e250fa32ea8b1313cf8bb7c2ad7627fd",
	"eba74343e2f15d62adedfd8c883ee0262b5c8021",
	"c2d30fa8ef288618f65f6eed6e168e0d514886f4",
	"8dcef98b1d52143e1e2dbc458ffe38f925786bf2",
	"aa9b383c260e1d05fbbf6b30a02914555e20c725",
	"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	"dbd3641b371024f44d0e469a9c8f5457b0660de1",
	"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	"7e59600739c96546163833214c36459e324bad0a",
}

var expectedEntries = map[plumbing.Hash]int64{
	plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"): 615,
	plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88"): 1524,
	plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"): 1063,
	plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9"): 78882,
	plumbing.NewHash("4d081c50e250fa32ea8b1313cf8bb7c2ad7627fd"): 84688,
	plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa"): 84559,
	plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda"): 84479,
	plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"): 186,
	plumbing.NewHash("7e59600739c96546163833214c36459e324bad0a"): 84653,
	plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"): 78050,
	plumbing.NewHash("8dcef98b1d52143e1e2dbc458ffe38f925786bf2"): 84741,
	plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"): 286,
	plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492"): 80998,
	plumbing.NewHash("9dea2395f5403188298c1dabe8bdafe562c491e3"): 84032,
	plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db"): 84430,
	plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"): 838,
	plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"): 84375,
	plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725"): 84760,
	plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"): 449,
	plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"): 1392,
	plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"): 1230,
	plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f"): 1713,
	plumbing.NewHash("c2d30fa8ef288618f65f6eed6e168e0d514886f4"): 84725,
	plumbing.NewHash("c8f1d8c61f9da76f4cb49fd86322b6e685dba956"): 80725,
	plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b"): 84608,
	plumbing.NewHash("d3ff53e0564a9f87d8e84b6e28e5060e517008aa"): 1685,
	plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d"): 2351,
	plumbing.NewHash("dbd3641b371024f44d0e469a9c8f5457b0660de1"): 84115,
	plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"): 12,
	plumbing.NewHash("eba74343e2f15d62adedfd8c883ee0262b5c8021"): 84708,
	plumbing.NewHash("fb72698cab7617ac416264415f13224dfd7a165e"): 84671,
}
