package object

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/x/plugin"
)

var benchSignature = []byte("-----BEGIN PGP SIGNATURE-----\n\nabcdefghijklmnopqrstuvwxyz\n-----END PGP SIGNATURE-----\n")

// hashVerifier mimics what real verifiers (openpgp, sshsig) do: stream the
// message through a hash. It performs no cryptographic check.
type hashVerifier struct{}

func (hashVerifier) Verify(_ context.Context, message io.Reader, _ []byte) (*plugin.Verification, error) {
	if _, err := io.Copy(sha256.New(), message); err != nil {
		return nil, err
	}
	return &plugin.Verification{}, nil
}

func benchSignedCommit(tb testing.TB, bodySize int) *Commit {
	tb.Helper()
	c := &Commit{
		Author:    Signature{Name: "go-git", Email: "go-git@example.com"},
		Committer: Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:   strings.Repeat("a", bodySize) + "\n",
		TreeHash:  plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
		Signature: benchSignature,
	}
	enc := &plumbing.MemoryObject{}
	if err := c.Encode(enc); err != nil {
		tb.Fatal(err)
	}
	decoded := &Commit{}
	if err := decoded.Decode(enc); err != nil {
		tb.Fatal(err)
	}
	return decoded
}

func benchSignedTag(tb testing.TB, bodySize int) *Tag {
	tb.Helper()
	tag := &Tag{
		Name:       "v1",
		Tagger:     Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    strings.Repeat("a", bodySize) + "\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature:  benchSignature,
	}
	enc := &plumbing.MemoryObject{}
	if err := tag.Encode(enc); err != nil {
		tb.Fatal(err)
	}
	decoded := &Tag{}
	if err := decoded.Decode(enc); err != nil {
		tb.Fatal(err)
	}
	return decoded
}

// payloadLen returns the number of bytes the verifier processes for obj (the
// signature-stripped payload), used as the throughput basis via b.SetBytes.
func payloadLen(tb testing.TB, obj interface {
	EncodeWithoutSignature() (io.Reader, error)
},
) int64 {
	tb.Helper()
	r, err := obj.EncodeWithoutSignature()
	if err != nil {
		tb.Fatal(err)
	}
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		tb.Fatal(err)
	}
	return n
}

var benchSizes = []int{200, 4 << 10, 256 << 10}

func BenchmarkCommitVerify(b *testing.B) {
	v := hashVerifier{}
	for _, size := range benchSizes {
		c := benchSignedCommit(b, size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.SetBytes(payloadLen(b, c))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := c.Verify(context.Background(), WithVerifier(v)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTagVerify(b *testing.B) {
	v := hashVerifier{}
	for _, size := range benchSizes {
		tag := benchSignedTag(b, size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.SetBytes(payloadLen(b, tag))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := tag.Verify(context.Background(), WithVerifier(v)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
