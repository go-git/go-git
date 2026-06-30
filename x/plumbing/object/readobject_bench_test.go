package object_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	gitobject "github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/x/plugin"
	"github.com/go-git/go-git/v6/x/plumbing/object"
)

var benchSignature = []byte("-----BEGIN PGP SIGNATURE-----\n\nabcdefghijklmnopqrstuvwxyz\n-----END PGP SIGNATURE-----\n")

var benchSizes = []int{200, 4 << 10, 256 << 10}

// hashVerifier mimics what real verifiers (openpgp, sshsig) do: stream the
// message through a hash. It performs no cryptographic check.
type hashVerifier struct{}

func (hashVerifier) Verify(_ context.Context, message io.Reader, _ []byte) (*plugin.Verification, error) {
	if _, err := io.Copy(sha256.New(), message); err != nil {
		return nil, err
	}
	return &plugin.Verification{}, nil
}

func benchEncodedCommit(tb testing.TB, bodySize int) *plumbing.MemoryObject {
	tb.Helper()
	c := &gitobject.Commit{
		Author:    gitobject.Signature{Name: "go-git", Email: "go-git@example.com"},
		Committer: gitobject.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:   strings.Repeat("a", bodySize) + "\n",
		TreeHash:  plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
		ParentHashes: []plumbing.Hash{
			plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		},
		Signature: benchSignature,
	}
	enc := &plumbing.MemoryObject{}
	if err := c.Encode(enc); err != nil {
		tb.Fatal(err)
	}
	return enc
}

func benchEncodedTag(tb testing.TB, bodySize int) *plumbing.MemoryObject {
	tb.Helper()
	tag := &gitobject.Tag{
		Name:       "v1",
		Tagger:     gitobject.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    strings.Repeat("a", bodySize) + "\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  benchSignature,
	}
	enc := &plumbing.MemoryObject{}
	if err := tag.Encode(enc); err != nil {
		tb.Fatal(err)
	}
	return enc
}

func benchReadCommit(tb testing.TB, bodySize int) *object.ReadCommit {
	tb.Helper()
	rc, err := object.DecodeReadCommit(nil, benchEncodedCommit(tb, bodySize))
	if err != nil {
		tb.Fatal(err)
	}
	return rc
}

func benchReadTag(tb testing.TB, bodySize int) *object.ReadTag {
	tb.Helper()
	rt, err := object.DecodeReadTag(nil, benchEncodedTag(tb, bodySize))
	if err != nil {
		tb.Fatal(err)
	}
	return rt
}

// BenchmarkReadCommitVerify exercises the immutable verify path: because a
// ReadCommit cannot be mutated, it streams the signed payload straight from the
// stored source bytes, so memory use is constant regardless of body size.
func BenchmarkReadCommitVerify(b *testing.B) {
	v := hashVerifier{}
	for _, size := range benchSizes {
		rc := benchReadCommit(b, size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := rc.Verify(context.Background(), gitobject.WithVerifier(v)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReadTagVerify(b *testing.B) {
	v := hashVerifier{}
	for _, size := range benchSizes {
		rt := benchReadTag(b, size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := rt.Verify(context.Background(), gitobject.WithVerifier(v)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeReadCommit(b *testing.B) {
	for _, size := range benchSizes {
		enc := benchEncodedCommit(b, size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.SetBytes(enc.Size())
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := object.DecodeReadCommit(nil, enc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeReadTag(b *testing.B) {
	for _, size := range benchSizes {
		enc := benchEncodedTag(b, size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.SetBytes(enc.Size())
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := object.DecodeReadTag(nil, enc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
