package commitgraph_test

import (
	"bytes"
	encbin "encoding/binary"
	"path"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// Commits of the commit-graph-bloom-* fixtures, oldest first.
const (
	bloomRoot         = "cc41864b750b4897678e471c414ad9cb791c9917"
	bloomModifyC      = "fd51a245eb11707fc4d78cccd8009bc4132e55bd"
	bloomModifyReadme = "e045ef64c1aa88c8fcbc90b87fadf1041a7587e4"
	bloomEmpty        = "4fa1388cd666ef0e04c578f07344d64d889ade70"
	bloomAddHighBit   = "bbf7d7950444f7f460644336ee4839195cc96433"
	bloomAdd513Files  = "daaf7f0040f24005a92c265d51a6bc50c35ff51e"
	bloomModifyCAgain = "0610ebd8450cba55af358408b6222280ae0dc396"
	bloomHighBitPath  = "\xc2\xa2"
)

func bloomDotGit(t *testing.T, tag string) billy.Filesystem {
	t.Helper()
	dotgit, err := fixtures.ByTag(tag).One().DotGit(fixtures.WithMemFS())
	require.NoError(t, err)
	return dotgit
}

func openBloomGraph(t *testing.T, tag string) commitgraph.Index {
	t.Helper()
	index, err := commitgraph.OpenChainOrFileIndex(bloomDotGit(t, tag))
	require.NoError(t, err)
	t.Cleanup(func() { _ = index.Close() })
	return index
}

func readBloomV2Graph(t *testing.T) []byte {
	t.Helper()
	raw, err := util.ReadFile(bloomDotGit(t, "commit-graph-bloom-v2"), path.Join("objects", "info", "commit-graph"))
	require.NoError(t, err)
	return raw
}

func bloomFilterOf(t *testing.T, index commitgraph.Index, commit string) *commitgraph.BloomFilter {
	t.Helper()
	h, _ := plumbing.FromHex(commit)
	i, err := index.GetIndexByHash(h)
	require.NoError(t, err)
	filter, err := index.GetBloomFilterByIndex(i)
	require.NoError(t, err)
	return filter
}

func TestBloomFilterFromGitGraph(t *testing.T) {
	t.Parallel()

	tests := []struct {
		commit    string
		changed   []string
		unchanged []string
	}{
		{bloomRoot, []string{"README", "a", "a/b", "a/b/", "a/b/c.txt"}, nil},
		{bloomModifyC, []string{"a/b/c.txt", "a/b"}, []string{"README"}},
		{bloomModifyReadme, []string{"README"}, []string{"a/b/c.txt", "a"}},
		{bloomEmpty, nil, []string{"README", "a/b/c.txt"}},
		{bloomAddHighBit, []string{bloomHighBitPath}, []string{"README"}},
		// More than 512 changes: Git stores a filter with every bit set.
		{bloomAdd513Files, []string{"big/0", "big", "README"}, nil},
		{bloomModifyCAgain, []string{"a/b/c.txt"}, []string{"README"}},
	}
	for _, tag := range []string{"commit-graph-bloom-v1", "commit-graph-bloom-v2"} {
		index := openBloomGraph(t, tag)
		for _, tc := range tests {
			filter := bloomFilterOf(t, index, tc.commit)
			require.NotNil(t, filter, "%s %s", tag, tc.commit)
			for _, p := range tc.changed {
				assert.True(t, filter.MaybeContains(p), "%s %s %q", tag, tc.commit, p)
			}
			for _, p := range tc.unchanged {
				assert.False(t, filter.MaybeContains(p), "%s %s %q", tag, tc.commit, p)
			}
		}
	}
}

func TestBloomFilterHighBitPathOnlyRuledOutByVersion2(t *testing.T) {
	t.Parallel()

	v1 := bloomFilterOf(t, openBloomGraph(t, "commit-graph-bloom-v1"), bloomModifyC)
	assert.True(t, v1.MaybeContains(bloomHighBitPath))

	v2 := bloomFilterOf(t, openBloomGraph(t, "commit-graph-bloom-v2"), bloomModifyC)
	assert.False(t, v2.MaybeContains(bloomHighBitPath))
}

func TestBloomFilterChainUsesNewestLayerSettings(t *testing.T) {
	t.Parallel()

	dotgit := bloomDotGit(t, "commit-graph-bloom-chain")
	index, err := commitgraph.OpenChainIndex(dotgit)
	require.NoError(t, err)
	t.Cleanup(func() { _ = index.Close() })

	for _, commit := range []string{bloomRoot, bloomModifyC, bloomModifyReadme} {
		assert.Nil(t, bloomFilterOf(t, index, commit), "base layer %s", commit)
	}
	assert.Nil(t, bloomFilterOf(t, index, bloomEmpty), "layer without filters")

	top := bloomFilterOf(t, index, bloomModifyCAgain)
	require.NotNil(t, top)
	assert.True(t, top.MaybeContains("a/b/c.txt"))
	assert.False(t, top.MaybeContains("README"))

	graphs := path.Join("objects", "info", "commit-graphs")
	chainFile, err := dotgit.Open(path.Join(graphs, "commit-graph-chain"))
	require.NoError(t, err)
	chain, err := commitgraph.OpenChainFile(chainFile)
	require.NoError(t, err)
	require.NoError(t, chainFile.Close())

	raw, err := util.ReadFile(dotgit, path.Join(graphs, "graph-"+chain[0]+".graph"))
	require.NoError(t, err)
	base, err := openIndexBytes(raw)
	require.NoError(t, err)
	baseFilter := bloomFilterOf(t, base, bloomModifyC)
	require.NotNil(t, baseFilter, "base layer read on its own")
	assert.True(t, baseFilter.MaybeContains("a/b/c.txt"))
}

func TestBloomFilterIgnoresUnknownHashVersion(t *testing.T) {
	t.Parallel()

	raw := readBloomV2Graph(t)
	bdat, ok := findTOCOffset(raw, []byte("BDAT"))
	require.True(t, ok)
	encbin.BigEndian.PutUint32(raw[bdat:], 3)

	index, err := openIndexBytes(raw)
	require.NoError(t, err)
	assert.Nil(t, bloomFilterOf(t, index, bloomModifyC))
}

func TestBloomFilterIgnoresExcessiveNumHashes(t *testing.T) {
	t.Parallel()

	raw := readBloomV2Graph(t)
	bdat, ok := findTOCOffset(raw, []byte("BDAT"))
	require.True(t, ok)
	encbin.BigEndian.PutUint32(raw[bdat+4:], 0xffffffff)

	index, err := openIndexBytes(raw)
	require.NoError(t, err)
	assert.Nil(t, bloomFilterOf(t, index, bloomModifyC))
}

func TestBloomFilterIgnoresWronglySizedIndexChunk(t *testing.T) {
	t.Parallel()

	raw := readBloomV2Graph(t)
	bdat, ok := findTOCOffset(raw, []byte("BDAT"))
	require.True(t, ok)
	require.True(t, patchTOCOffset(raw, []byte("BDAT"), bdat-4))

	index, err := openIndexBytes(raw)
	require.NoError(t, err)
	assert.Nil(t, bloomFilterOf(t, index, bloomModifyC))
	_, err = index.GetCommitDataByIndex(0)
	assert.NoError(t, err)
}

func TestBloomFilterIgnoresInvalidIndexOffsets(t *testing.T) {
	t.Parallel()

	tests := map[string]uint32{
		"past BDAT chunk": 0x7fffffff,
		"decreasing":      0,
	}
	for name, offset := range tests {
		raw := readBloomV2Graph(t)
		index, err := openIndexBytes(raw)
		require.NoError(t, err)
		// bloomModifyC sorts last and bloomModifyCAgain first, so only
		// the former's filter bounds are touched.
		h, _ := plumbing.FromHex(bloomModifyC)
		i, err := index.GetIndexByHash(h)
		require.NoError(t, err)
		require.NotZero(t, i)
		bidx, ok := findTOCOffset(raw, []byte("BIDX"))
		require.True(t, ok)
		encbin.BigEndian.PutUint32(raw[bidx+4*uint64(i):], offset)

		index, err = openIndexBytes(raw)
		require.NoError(t, err)
		assert.Nil(t, bloomFilterOf(t, index, bloomModifyC), name)
		assert.NotNil(t, bloomFilterOf(t, index, bloomModifyCAgain), name)
	}
}

func TestBloomFilterHasNoFalseNegatives(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"commit-graph-bloom-v1", "commit-graph-bloom-v2"} {
		dotgit := bloomDotGit(t, tag)
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		require.NoError(t, err)
		t.Cleanup(func() { _ = index.Close() })
		storer := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

		for i, hash := range index.Hashes() {
			commit, err := object.GetCommit(storer, hash)
			require.NoError(t, err)
			tree, err := commit.Tree()
			require.NoError(t, err)
			var parentTree *object.Tree
			if commit.NumParents() > 0 {
				parent, err := commit.Parent(0)
				require.NoError(t, err)
				parentTree, err = parent.Tree()
				require.NoError(t, err)
			}
			changes, err := object.DiffTree(parentTree, tree)
			require.NoError(t, err)

			filter, err := index.GetBloomFilterByIndex(uint32(i))
			require.NoError(t, err)
			require.NotNil(t, filter, "%s %s", tag, hash)
			for _, change := range changes {
				for _, name := range []string{change.From.Name, change.To.Name} {
					if name != "" {
						assert.True(t, filter.MaybeContains(name), "%s %s %q", tag, hash, name)
					}
				}
			}
		}
	}
}

func TestBloomFilterNotComputedForZeroLengthFilter(t *testing.T) {
	t.Parallel()

	raw := readBloomV2Graph(t)
	index, err := openIndexBytes(raw)
	require.NoError(t, err)
	h, _ := plumbing.FromHex(bloomModifyC)
	i, err := index.GetIndexByHash(h)
	require.NoError(t, err)
	require.NotZero(t, i)
	bidx, ok := findTOCOffset(raw, []byte("BIDX"))
	require.True(t, ok)
	// Git leaves a commit's filter empty when it was not computed, for
	// example past `--max-new-filters`.
	previousEnd := encbin.BigEndian.Uint32(raw[bidx+4*uint64(i-1):])
	encbin.BigEndian.PutUint32(raw[bidx+4*uint64(i):], previousEnd)

	index, err = openIndexBytes(raw)
	require.NoError(t, err)
	assert.Nil(t, bloomFilterOf(t, index, bloomModifyC))
}

func TestBloomFilterChainFallsBackToLowerLayerSettings(t *testing.T) {
	t.Parallel()

	dotgit := bloomDotGit(t, "commit-graph-bloom-chain")
	graphs := path.Join("objects", "info", "commit-graphs")
	chainFile, err := dotgit.Open(path.Join(graphs, "commit-graph-chain"))
	require.NoError(t, err)
	chain, err := commitgraph.OpenChainFile(chainFile)
	require.NoError(t, err)
	require.NoError(t, chainFile.Close())

	baseRaw, err := util.ReadFile(dotgit, path.Join(graphs, "graph-"+chain[0]+".graph"))
	require.NoError(t, err)
	base, err := openIndexBytes(baseRaw)
	require.NoError(t, err)
	withoutFiltersRaw, err := util.ReadFile(dotgit, path.Join(graphs, "graph-"+chain[1]+".graph"))
	require.NoError(t, err)
	index, err := commitgraph.OpenFileIndexWithParent(discardCloseReader{bytes.NewReader(withoutFiltersRaw)}, base)
	require.NoError(t, err)

	assert.Nil(t, bloomFilterOf(t, index, bloomEmpty), "layer without filters")
	filter := bloomFilterOf(t, index, bloomModifyC)
	require.NotNil(t, filter, "base layer, the newest one with filters")
	assert.True(t, filter.MaybeContains("a/b/c.txt"))
}

func TestBloomFilterIndexOutOfRange(t *testing.T) {
	t.Parallel()

	index := openBloomGraph(t, "commit-graph-bloom-v2")
	_, err := index.GetBloomFilterByIndex(index.MaximumNumberOfHashes())
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	filter, err := commitgraph.NewMemoryIndex().GetBloomFilterByIndex(0)
	assert.NoError(t, err)
	assert.Nil(t, filter)
}
