package index

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

var (
	cacheTreeBlob = plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")
	cacheTreeRoot = plumbing.NewHash("1111111111111111111111111111111111111111")
	cacheTreeA    = plumbing.NewHash("2222222222222222222222222222222222222222")
	cacheTreeAB   = plumbing.NewHash("3333333333333333333333333333333333333333")
	cacheTreeC    = plumbing.NewHash("4444444444444444444444444444444444444444")
)

// cacheTreeIndex is an index of a/b/f, a/g, c/h and top, with a cache-tree
// valid for every directory.
func cacheTreeIndex() *index.Index {
	return &index.Index{
		Entries: []*index.Entry{
			{Name: "a/b/f", Hash: cacheTreeBlob, Mode: filemode.Regular},
			{Name: "a/g", Hash: cacheTreeBlob, Mode: filemode.Regular},
			{Name: "c/h", Hash: cacheTreeBlob, Mode: filemode.Regular},
			{Name: "top", Hash: cacheTreeBlob, Mode: filemode.Regular},
		},
		Cache: &index.Tree{Entries: []index.TreeEntry{
			{Path: "", Entries: 4, Trees: 2, Hash: cacheTreeRoot},
			{Path: "a", Entries: 2, Trees: 1, Hash: cacheTreeA},
			{Path: "b", Entries: 1, Trees: 0, Hash: cacheTreeAB},
			{Path: "c", Entries: 1, Trees: 0, Hash: cacheTreeC},
		}},
	}
}

// dirHashes returns the hash of every directory below root by path.
func dirHashes(t *testing.T, root noder.Noder) map[string][]byte {
	t.Helper()

	hashes := map[string][]byte{}
	nodes := []noder.Noder{root}
	for len(nodes) > 0 {
		n := nodes[0]
		nodes = nodes[1:]
		if !n.IsDir() {
			continue
		}
		hashes[n.String()] = n.Hash()
		children, err := n.Children()
		require.NoError(t, err)
		nodes = append(nodes, children...)
	}
	return hashes
}

func treeHash(h plumbing.Hash) []byte {
	return append(h.Bytes(), filemode.Dir.Bytes()...)
}

var zeroDirHash = make([]byte, 24)

func TestCacheTreeHashesDirectories(t *testing.T) {
	t.Parallel()

	got := dirHashes(t, NewRootNodeWithOptions(cacheTreeIndex(), RootNodeOptions{CacheTree: true}))
	assert.Equal(t, map[string][]byte{
		"":    treeHash(cacheTreeRoot),
		"a":   treeHash(cacheTreeA),
		"a/b": treeHash(cacheTreeAB),
		"c":   treeHash(cacheTreeC),
	}, got)
}

func TestCacheTreeIsOptIn(t *testing.T) {
	t.Parallel()

	for path, h := range dirHashes(t, NewRootNode(cacheTreeIndex())) {
		assert.Equal(t, zeroDirHash, h, path)
	}
}

func TestCacheTreeIgnoresEntriesItCannotTrust(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*index.Index)
		want   map[string][]byte
	}{
		{
			name: "invalidated entry keeps its subtrees in place",
			change: func(idx *index.Index) {
				idx.Cache.Entries[0] = index.TreeEntry{Entries: -1, Trees: 2}
				idx.Cache.Entries[1] = index.TreeEntry{Path: "a", Entries: -1, Trees: 1}
			},
			want: map[string][]byte{
				"": zeroDirHash, "a": zeroDirHash,
				"a/b": treeHash(cacheTreeAB), "c": treeHash(cacheTreeC),
			},
		},
		{
			name: "entry count differs from the index",
			change: func(idx *index.Index) {
				idx.Entries = append(idx.Entries, &index.Entry{Name: "c/new", Hash: cacheTreeBlob})
			},
			want: map[string][]byte{
				"": zeroDirHash, "a": treeHash(cacheTreeA),
				"a/b": treeHash(cacheTreeAB), "c": zeroDirHash,
			},
		},
		{
			name: "conflict stage below the directory",
			change: func(idx *index.Index) {
				idx.Entries[2].Stage = index.OurMode
			},
			want: map[string][]byte{
				"": zeroDirHash, "a": treeHash(cacheTreeA),
				"a/b": treeHash(cacheTreeAB), "c": zeroDirHash,
			},
		},
		{
			name: "intent to add below the directory",
			change: func(idx *index.Index) {
				idx.Entries[0].IntentToAdd = true
			},
			want: map[string][]byte{
				"": zeroDirHash, "a": zeroDirHash,
				"a/b": zeroDirHash, "c": treeHash(cacheTreeC),
			},
		},
		{
			name: "subtree count past the end",
			change: func(idx *index.Index) {
				idx.Cache.Entries[3].Trees = 1
			},
			want: map[string][]byte{"": zeroDirHash, "a": zeroDirHash, "a/b": zeroDirHash, "c": zeroDirHash},
		},
		{
			name: "entries left over",
			change: func(idx *index.Index) {
				idx.Cache.Entries = append(idx.Cache.Entries, index.TreeEntry{Path: "d", Entries: 1, Hash: cacheTreeC})
			},
			want: map[string][]byte{"": zeroDirHash, "a": zeroDirHash, "a/b": zeroDirHash, "c": zeroDirHash},
		},
		{
			name: "subtree name with a slash",
			change: func(idx *index.Index) {
				idx.Cache.Entries[2].Path = "x/b"
			},
			want: map[string][]byte{"": zeroDirHash, "a": zeroDirHash, "a/b": zeroDirHash, "c": zeroDirHash},
		},
		{
			name: "root with a name",
			change: func(idx *index.Index) {
				idx.Cache.Entries[0].Path = "a"
			},
			want: map[string][]byte{"": zeroDirHash, "a": zeroDirHash, "a/b": zeroDirHash, "c": zeroDirHash},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idx := cacheTreeIndex()
			tc.change(idx)
			got := dirHashes(t, NewRootNodeWithOptions(idx, RootNodeOptions{CacheTree: true}))
			assert.Equal(t, tc.want, got)
		})
	}
}

// A cache-tree nested far deeper than the index, which a crafted TREE
// extension can hold at a few bytes per level, is walked in time and memory
// linear in its length: paths are built only for directories of the index.
func TestCacheTreeDeepChainStaysCheap(t *testing.T) {
	t.Parallel()

	// The bound leaves room for the other tests of the package, which run
	// alongside and allocate little; the quadratic walk took about 5 GB.
	const depth = 50000

	entries := make([]index.TreeEntry, depth)
	for i := range entries {
		entries[i] = index.TreeEntry{Path: "a", Entries: -1, Trees: 1}
	}
	entries[0].Path = ""
	entries[depth-1].Trees = 0
	idx := &index.Index{
		Entries: []*index.Entry{{Name: "a/f", Hash: cacheTreeBlob, Mode: filemode.Regular}},
		Cache:   &index.Tree{Entries: entries},
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := dirHashes(t, NewRootNodeWithOptions(idx, RootNodeOptions{CacheTree: true}))
	runtime.ReadMemStats(&after)

	assert.Equal(t, map[string][]byte{"": zeroDirHash, "a": zeroDirHash}, got)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(32<<20))
}
