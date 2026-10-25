package commitgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Expected values come from Git's t/t0095-bloom.sh.

func TestMurmur3(t *testing.T) {
	t.Parallel()

	tests := []struct {
		data string
		want uint32
	}{
		{"", 0x00000000},
		{"Hello world!", 0x627b0c2c},
		{"The quick brown fox jumps over the lazy dog", 0x2e4ff723},
		{"\x99\xaa\xbb\xcc\xdd\xee\xff", 0xa183ccfd},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, murmur3(0, tc.data), "%q", tc.data)
	}
}

func TestBloomFilterMaybeContainsGitFilters(t *testing.T) {
	t.Parallel()

	defaultSettings := bloomSettings{hashVersion: 2, numHashes: 7, bitsPerEntry: 10}
	tests := []struct {
		path string
		data []byte
	}{
		{" ", []byte{0x51, 0x55}},
		{"Hello world!", []byte{0x92, 0x6c}},
		{"file.txt", []byte{0xa5, 0x4a}},
	}
	for _, tc := range tests {
		f := &BloomFilter{data: tc.data, settings: defaultSettings}
		assert.True(t, f.MaybeContains(tc.path), "%q", tc.path)
	}

	noChanges := &BloomFilter{data: []byte{0x00}, settings: defaultSettings}
	assert.False(t, noChanges.MaybeContains("file.txt"))
}

func TestBloomFilterMaybeContainsRequiresLeadingDirectories(t *testing.T) {
	t.Parallel()

	settings := bloomSettings{hashVersion: 2, numHashes: 7, bitsPerEntry: 10}
	f := &BloomFilter{data: make([]byte, 8), settings: settings}
	add := func(key string) {
		h0, h1 := murmur3(bloomSeed0, key), murmur3(bloomSeed1, key)
		for i := range settings.numHashes {
			pos := uint64(h0+i*h1) % uint64(len(f.data)*8)
			f.data[pos/8] |= 1 << (pos % 8)
		}
	}

	add("a/b/c.txt")
	assert.False(t, f.MaybeContains("a/b/c.txt"))
	add("a/b")
	add("a")
	assert.True(t, f.MaybeContains("a/b/c.txt"))
	assert.True(t, f.MaybeContains("a/b/"))
}

func TestBloomFilterMaybeContainsHighBitPathWithVersion1(t *testing.T) {
	t.Parallel()

	v1 := &BloomFilter{data: []byte{0x00}, settings: bloomSettings{hashVersion: 1, numHashes: 7}}
	assert.True(t, v1.MaybeContains("caf\xc3\xa9"))
	assert.False(t, v1.MaybeContains("cafe"))

	v2 := &BloomFilter{data: []byte{0x00}, settings: bloomSettings{hashVersion: 2, numHashes: 7}}
	assert.False(t, v2.MaybeContains("caf\xc3\xa9"))
}

func TestBloomFilterMaybeContainsWithoutFilter(t *testing.T) {
	t.Parallel()

	var f *BloomFilter
	assert.True(t, f.MaybeContains("file.txt"))
}

func TestBloomFilterMaybeContainsEmptyPath(t *testing.T) {
	t.Parallel()

	f := &BloomFilter{data: []byte{0x00}, settings: bloomSettings{hashVersion: 2, numHashes: 7}}
	assert.True(t, f.MaybeContains(""))
	assert.True(t, f.MaybeContains("/"))
}
