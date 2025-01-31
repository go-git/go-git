package packfile_test

import (
	"io"
	"testing"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
)

func TestParserHashes(t *testing.T) {
	tests := []struct {
		name    string
		storage storer.Storer
	}{
		{
			name: "without storage",
		},
		{
			name:    "with storage",
			storage: filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtures.Basic().One()

			obs := new(testObserver)
			parser := packfile.NewParser(f.Packfile(), packfile.WithScannerObservers(obs),
				packfile.WithStorage(tc.storage))

			commit := plumbing.CommitObject
			blob := plumbing.BlobObject
			tree := plumbing.TreeObject

			objs := []observerObject{
				{hash: "e8d3ffab552895c19b9fcf7aa264d277cde33881", otype: commit, size: 254, offset: 12, crc: 0xaa07ba4b},
				{hash: "918c48b83bd081e863dbe1b80f8998f058cd8294", otype: commit, size: 242, offset: 286, crc: 0x12438846},
				{hash: "af2d6a6954d532f8ffb47615169c8fdf9d383a1a", otype: commit, size: 242, offset: 449, crc: 0x2905a38c},
				{hash: "1669dce138d9b841a518c64b10914d88f5e488ea", otype: commit, size: 333, offset: 615, crc: 0xd9429436},
				{hash: "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69", otype: commit, size: 332, offset: 838, crc: 0xbecfde4e},
				{hash: "35e85108805c84807bc66a02d91535e1e24b38b9", otype: commit, size: 244, offset: 1063, crc: 0x780e4b3e},
				{hash: "b8e471f58bcbca63b07bda20e428190409c2db47", otype: commit, size: 243, offset: 1230, crc: 0xdc18344f},
				{hash: "b029517f6300c2da0f4b651b8642506cd6aaf45d", otype: commit, size: 187, offset: 1392, crc: 0xcf4e4280},
				{hash: "32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", otype: blob, size: 189, offset: 1524, crc: 0x1f08118a},
				{hash: "d3ff53e0564a9f87d8e84b6e28e5060e517008aa", otype: blob, size: 18, offset: 1685, crc: 0xafded7b8},
				{hash: "c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", otype: blob, size: 1072, offset: 1713, crc: 0xcc1428ed},
				{hash: "d5c0f4ab811897cadf03aec358ae60d21f91c50d", otype: blob, size: 76110, offset: 2351, crc: 0x1631d22f},
				{hash: "880cd14280f4b9b6ed3986d6671f907d7cc2a198", otype: blob, size: 2780, offset: 78050, crc: 0xbfff5850},
				{hash: "49c6bb89b17060d7b4deacb7b338fcc6ea2352a9", otype: blob, size: 217848, offset: 78882, crc: 0xd108e1d8},
				{hash: "c8f1d8c61f9da76f4cb49fd86322b6e685dba956", otype: blob, size: 706, offset: 80725, crc: 0x8e97ba25},
				{hash: "9a48f23120e880dfbe41f7c9b7b708e9ee62a492", otype: blob, size: 11488, offset: 80998, crc: 0x7316ff70},
				{hash: "9dea2395f5403188298c1dabe8bdafe562c491e3", otype: blob, size: 78, offset: 84032, crc: 0xdb4fce56},
				{hash: "dbd3641b371024f44d0e469a9c8f5457b0660de1", otype: tree, size: 272, offset: 84115, crc: 0x901cce2c},
				{hash: "a39771a7651f97faf5c72e08224d857fc35133db", otype: tree, size: 38, offset: 84430, crc: 0x847905bf},
				{hash: "5a877e6a906a2743ad6e45d99c1793642aaf8eda", otype: tree, size: 75, offset: 84479, crc: 0x3689459a},
				{hash: "586af567d0bb5e771e49bdd9434f5e0fb76d25fa", otype: tree, size: 38, offset: 84559, crc: 0xe67af94a},
				{hash: "cf4aa3b38974fb7d81f367c0830f7d78d65ab86b", otype: tree, size: 34, offset: 84608, crc: 0xc2314a2e},
				{hash: "7e59600739c96546163833214c36459e324bad0a", otype: blob, size: 9, offset: 84653, crc: 0xcd987848},
				{hash: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", otype: commit, size: 245, offset: 186, crc: 0xf706df58},
				{hash: "a8d315b2b1c615d43042c3a62402b8a54288cf5c", otype: tree, size: 271, offset: 84375, crc: 0xec4552b0},
				{hash: "fb72698cab7617ac416264415f13224dfd7a165e", otype: tree, size: 238, offset: 84671, crc: 0x8a853a6d},
				{hash: "4d081c50e250fa32ea8b1313cf8bb7c2ad7627fd", otype: tree, size: 179, offset: 84688, crc: 0x70c6518},
				{hash: "eba74343e2f15d62adedfd8c883ee0262b5c8021", otype: tree, size: 148, offset: 84708, crc: 0x4f4108e2},
				{hash: "c2d30fa8ef288618f65f6eed6e168e0d514886f4", otype: tree, size: 110, offset: 84725, crc: 0xd6fe09e9},
				{hash: "8dcef98b1d52143e1e2dbc458ffe38f925786bf2", otype: tree, size: 111, offset: 84741, crc: 0xf07a2804},
				{hash: "aa9b383c260e1d05fbbf6b30a02914555e20c725", otype: tree, size: 73, offset: 84760, crc: 0x1d75d6be},
			}

			_, err := parser.Parse()
			assert.NoError(t, err)

			assert.Equal(t, "a3fed42da1e8189a077c0e6846c040dcf73fc9dd", obs.checksum)
			assert.Equal(t, objs, obs.objects)
		})
	}
}

func TestThinPack(t *testing.T) {
	// Initialize an empty repository
	r, err := git.PlainInit(t.TempDir(), true)
	assert.NoError(t, err)

	// Try to parse a thin pack without having the required objects in the repo to
	// see if the correct errors are returned
	thinpack := fixtures.ByTag("thinpack").One()
	parser := packfile.NewParser(thinpack.Packfile(), packfile.WithStorage(r.Storer)) // ParserWithStorage writes to the storer all parsed objects!
	assert.NoError(t, err)

	_, err = parser.Parse()
	assert.Equal(t, err, packfile.ErrReferenceDeltaNotFound)

	// start over with a clean repo
	r, err = git.PlainInit(t.TempDir(), true)
	assert.NoError(t, err)

	// Now unpack a base packfile into our empty repo:
	f := fixtures.ByURL("https://github.com/spinnaker/spinnaker.git").One()
	w, err := r.Storer.(storer.PackfileWriter).PackfileWriter()
	assert.NoError(t, err)
	_, err = io.Copy(w, f.Packfile())
	assert.NoError(t, err)
	assert.NoError(t, w.Close())

	// Check that the test object that will come with our thin pack is *not* in the repo
	_, err = r.Storer.EncodedObject(plumbing.CommitObject, plumbing.NewHash(thinpack.Head))
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	// Now unpack the thin pack:
	parser = packfile.NewParser(thinpack.Packfile(), packfile.WithStorage(r.Storer)) // ParserWithStorage writes to the storer all parsed objects!

	h, err := parser.Parse()
	assert.NoError(t, err)
	assert.Equal(t, plumbing.NewHash("1288734cbe0b95892e663221d94b95de1f5d7be8"), h)

	// Check that our test object is now accessible
	_, err = r.Storer.EncodedObject(plumbing.CommitObject, plumbing.NewHash(thinpack.Head))
	assert.NoError(t, err)
}

func TestResolveExternalRefsInThinPack(t *testing.T) {
	extRefsThinPack := fixtures.ByTag("codecommit").One().Packfile()

	parser := packfile.NewParser(extRefsThinPack)

	checksum, err := parser.Parse()
	assert.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, checksum)
}

func TestResolveExternalRefs(t *testing.T) {
	extRefsThinPack := fixtures.ByTag("delta-before-base").One().Packfile()

	parser := packfile.NewParser(extRefsThinPack)

	checksum, err := parser.Parse()
	assert.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, checksum)
}

func TestMemoryResolveExternalRefs(t *testing.T) {
	extRefsThinPack := fixtures.ByTag("delta-before-base").One().Packfile()

	parser := packfile.NewParser(extRefsThinPack, packfile.WithStorage(memory.NewStorage()))

	checksum, err := parser.Parse()
	assert.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, checksum)
}

func BenchmarkParseBasic(b *testing.B) {
	f := fixtures.Basic().One().Packfile()
	scanner := packfile.NewScanner(f)
	storage := filesystem.NewStorage(osfs.New(b.TempDir()), cache.NewObjectLRUDefault())

	b.Run("with storage", func(b *testing.B) {
		benchmarkParseBasic(b, f, scanner, packfile.WithStorage(storage))
	})
	b.Run("with memory storage", func(b *testing.B) {
		benchmarkParseBasic(b, f, scanner, packfile.WithStorage(memory.NewStorage()))
	})
	b.Run("without storage", func(b *testing.B) {
		benchmarkParseBasic(b, f, scanner)
	})
}

func benchmarkParseBasic(b *testing.B,
	f billy.File, scanner *packfile.Scanner,
	opts ...packfile.ParserOption) {
	for i := 0; i < b.N; i++ {
		f.Seek(0, io.SeekStart)
		scanner.Reset()
		parser := packfile.NewParser(scanner, opts...)

		checksum, err := parser.Parse()
		if err != nil {
			b.Fatal(err)
		}

		if checksum == plumbing.ZeroHash {
			b.Fatal("failed to parse")
		}
	}
}

func BenchmarkParse(b *testing.B) {
	for _, f := range fixtures.ByTag("packfile") {
		scanner := packfile.NewScanner(f.Packfile())

		b.Run(f.URL, func(b *testing.B) {
			benchmarkParseBasic(b, f.Packfile(), scanner)
		})
	}
}

type observerObject struct {
	hash   string
	otype  plumbing.ObjectType
	size   int64
	offset int64
	crc    uint32
}

type testObserver struct {
	count    uint32
	checksum string
	objects  []observerObject
	pos      map[int64]int
}

func (t *testObserver) OnHeader(count uint32) error {
	t.count = count
	t.pos = make(map[int64]int, count)
	return nil
}

func (t *testObserver) OnInflatedObjectHeader(otype plumbing.ObjectType, objSize int64, pos int64) error {
	o := t.get(pos)
	o.otype = otype
	o.size = objSize
	o.offset = pos

	t.put(pos, o)

	return nil
}

func (t *testObserver) OnInflatedObjectContent(h plumbing.Hash, pos int64, crc uint32, _ []byte) error {
	o := t.get(pos)
	o.hash = h.String()
	o.crc = crc

	t.put(pos, o)

	return nil
}

func (t *testObserver) OnFooter(h plumbing.Hash) error {
	t.checksum = h.String()
	return nil
}

func (t *testObserver) get(pos int64) observerObject {
	i, ok := t.pos[pos]
	if ok {
		return t.objects[i]
	}

	return observerObject{}
}

func (t *testObserver) put(pos int64, o observerObject) {
	i, ok := t.pos[pos]
	if ok {
		t.objects[i] = o
		return
	}

	t.pos[pos] = len(t.objects)
	t.objects = append(t.objects, o)
}
