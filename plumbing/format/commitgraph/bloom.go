package commitgraph

import (
	"math/bits"
	"strings"
)

const (
	// szBloomDataHeader is the length of the BDAT chunk header holding the
	// hash version, the number of hashes and the bits per entry.
	szBloomDataHeader = 3 * szUint32

	// maxBloomNumHashes bounds the number of hashes per path a filter may
	// declare. Git writes 7.
	maxBloomNumHashes = 64

	bloomSeed0 = 0x293ae76f
	bloomSeed1 = 0x7e646e2c
)

// bloomSettings holds the BDAT chunk header. Filters can only be queried
// with the settings they were written with.
type bloomSettings struct {
	hashVersion  uint32
	numHashes    uint32
	bitsPerEntry uint32
}

// BloomFilter is the changed-path Bloom filter of a commit. It records the
// paths that differ between the commit and its first parent, or the empty
// tree for a root commit, together with every leading directory of those
// paths.
type BloomFilter struct {
	data     []byte
	settings bloomSettings
}

// MaybeContains reports whether path may differ between the commit and its
// first parent. A false result means it definitely does not. path is
// slash-separated and relative to the root of the repository; a single
// trailing slash is ignored, so a directory can be queried too. A nil filter,
// as returned when a commit has none, reports every path as possibly changed.
//
// As in Git's bloom_keyvec_new [1], path and each of its leading directories
// must all be in the filter, which lowers the false positive rate.
//
// Version 1 filters hash bytes with the high bit set differently depending on
// the platform that wrote them [2], so any path containing such a byte may
// differ.
//
// [1]: https://github.com/git/git/blob/v2.53.0/bloom.c#L281-L319
// [2]: https://github.com/git/git/blob/v2.53.0/bloom.c#L167-L222
func (f *BloomFilter) MaybeContains(path string) bool {
	if f == nil {
		return true
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return true
	}
	if f.settings.hashVersion == 1 && hasHighBit(path) {
		return true
	}

	for {
		if !f.containsKey(path) {
			return false
		}
		i := strings.LastIndexByte(path, '/')
		if i <= 0 {
			return true
		}
		path = path[:i]
	}
}

// containsKey mirrors Git's bloom_key_fill and bloom_filter_contains.
func (f *BloomFilter) containsKey(key string) bool {
	h0 := murmur3(bloomSeed0, key)
	h1 := murmur3(bloomSeed1, key)
	mod := uint64(len(f.data)) * 8
	for i := range f.settings.numHashes {
		pos := uint64(h0+i*h1) % mod
		if f.data[pos/8]&(1<<(pos%8)) == 0 {
			return false
		}
	}
	return true
}

func hasHighBit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// murmur3 is the 32-bit MurmurHash3 (MurmurHash3_x86_32, public domain [1]),
// which Git uses for version 2 changed-path filters.
//
// [1]: https://github.com/aappleby/smhasher/blob/master/src/MurmurHash3.cpp
func murmur3(seed uint32, data string) uint32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
	)

	h := seed
	n := len(data) &^ 3
	for i := 0; i < n; i += 4 {
		k := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
		k *= c1
		k = bits.RotateLeft32(k, 15)
		k *= c2
		h ^= k
		h = bits.RotateLeft32(h, 13)*5 + 0xe6546b64
	}

	var k uint32
	switch len(data) & 3 {
	case 3:
		k ^= uint32(data[n+2]) << 16
		fallthrough
	case 2:
		k ^= uint32(data[n+1]) << 8
		fallthrough
	case 1:
		k ^= uint32(data[n])
		k *= c1
		k = bits.RotateLeft32(k, 15)
		k *= c2
		h ^= k
	}

	h ^= uint32(len(data))
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}
