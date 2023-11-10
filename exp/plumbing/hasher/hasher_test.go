package hasher_test

import (
	"bytes"
	"crypto"
	"fmt"
	"hash"
	"strings"
	"sync"
	"testing"

	. "github.com/go-git/go-git/v5/exp/plumbing/hasher"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/pjbgf/sha1cd"
	"github.com/stretchr/testify/assert"
)

func TestHasher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		h       hash.Hash
		ot      plumbing.ObjectType
		content []byte
		want    string
	}{
		{
			"blob object sha1", crypto.SHA1.New(),
			plumbing.BlobObject,
			[]byte("hash object sample"),
			"9f361d484fcebb869e1919dc7467b82ac6ca5fad",
		},
		{
			"blob object sha256", crypto.SHA256.New(),
			plumbing.BlobObject,
			[]byte("hash object sample"),
			"2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe677",
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s:%q", tc.name, ""), func(t *testing.T) {
			oh, err := FromHash(tc.h)
			assert.NoError(t, err)

			h, err := oh.Compute(tc.ot, tc.content)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, h.String())
		})
	}
}

func TestMultipleHashes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		h        hash.Hash
		ot       plumbing.ObjectType
		content1 []byte
		content2 []byte
		want1    string
		want2    string
	}{
		{
			"reuse sha1 hasher instance for two ops", crypto.SHA1.New(),
			plumbing.BlobObject,
			[]byte("hash object sample"),
			[]byte("other object content"),
			"9f361d484fcebb869e1919dc7467b82ac6ca5fad",
			"e8bb453830a9efdfe4785275b92eb0766da3a73d",
		},
		{
			"reuse sha256 hasher instance for two ops", crypto.SHA256.New(),
			plumbing.BlobObject,
			[]byte("hash object sample"),
			[]byte("other object content"),
			"2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe677",
			"2f1eb67dc531a48962e741b61e88ef94cf70969bc6442a91cdcad7f5192e8c1d",
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s:%q", tc.name, ""), func(t *testing.T) {
			oh, err := FromHash(tc.h)
			assert.NoError(t, err)

			h, err := oh.Compute(tc.ot, tc.content1)
			assert.NoError(t, err)
			assert.Equal(t, tc.want1, h.String())

			h, err = oh.Compute(tc.ot, tc.content2)
			assert.NoError(t, err)
			assert.Equal(t, tc.want2, h.String())
		})
	}
}

func TestThreadSatefy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		h       hash.Hash
		ot      plumbing.ObjectType
		content []byte
		count   int
		want    string
	}{
		{
			"thread safety sha1", crypto.SHA1.New(),
			plumbing.BlobObject,
			bytes.Repeat([]byte{2}, 500),
			20,
			"147979c263be42345f0721a22c5339492aadd0bf",
		},
		{
			"thread safety sha256", crypto.SHA256.New(),
			plumbing.BlobObject,
			bytes.Repeat([]byte{2}, 500),
			20,
			"43196946e1d64387caaac746132f22c2be6f9a16914dad0231b479e16b9c3a01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oh, err := FromHash(tc.h)
			assert.NoError(t, err)

			var wg sync.WaitGroup
			for i := 0; i < tc.count; i++ {
				wg.Add(1)
				go func() {
					h, err := oh.Compute(tc.ot, tc.content)
					assert.NoError(t, err)

					got := h.String()
					assert.Equal(t, tc.want, got, "resulting hash impacted by race condition")
					wg.Done()
				}()
			}
			wg.Wait()
		})
	}
}

func BenchmarkHasher(b *testing.B) {
	qtds := []int64{100, 5000}

	for _, q := range qtds {
		b.Run(fmt.Sprintf("hasher-sha1-%dB", q), func(b *testing.B) {
			benchmarkHasher(b, q)
		})
		b.Run(fmt.Sprintf("objecthash-sha1-%dB", q), func(b *testing.B) {
			benchmarkObjectHash(b, sha1cd.New(), q)
		})
		b.Run(fmt.Sprintf("objecthash-sha256-%dB", q), func(b *testing.B) {
			benchmarkObjectHash(b, crypto.SHA256.New(), q)
		})
	}
}

func benchmarkHasher(b *testing.B, sz int64) {
	content := strings.Repeat("s", int(sz))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := plumbing.NewHasher(plumbing.BlobObject, sz)
		_, _ = h.Write([]byte(content))
		b.SetBytes(sz)
	}
}

func benchmarkObjectHash(b *testing.B, h hash.Hash, sz int64) {
	content := bytes.Repeat([]byte("s"), int(sz))
	oh, err := FromHash(h)
	assert.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = oh.Compute(plumbing.BlobObject, content)
		b.SetBytes(sz)
	}
}
