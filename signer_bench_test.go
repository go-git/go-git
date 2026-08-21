package git

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// noopSigner drains the message (as a real signer would, to hash it) but
// performs no cryptography. It isolates the internal signing process — building
// the bytes to be signed — from the signing primitive itself.
type noopSigner struct{}

func (noopSigner) Sign(_ context.Context, message io.Reader) ([]byte, error) {
	if _, err := io.Copy(io.Discard, message); err != nil {
		return nil, err
	}
	return nil, nil
}

// payloadLen returns the number of bytes the signer processes for obj (the
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

func benchUnsignedCommit(bodySize int) *object.Commit {
	return &object.Commit{
		Author:    object.Signature{Name: "go-git", Email: "go-git@example.com"},
		Committer: object.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:   strings.Repeat("a", bodySize) + "\n",
		TreeHash:  plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
	}
}

func benchUnsignedTag(bodySize int) *object.Tag {
	return &object.Tag{
		Name:       "v1",
		Tagger:     object.Signature{Name: "go-git", Email: "go-git@example.com"},
		Message:    strings.Repeat("a", bodySize) + "\n",
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
	}
}

var signBenchSizes = []int{200, 4 << 10, 256 << 10}

func BenchmarkSignCommit(b *testing.B) {
	signer := noopSigner{}
	for _, size := range signBenchSizes {
		c := benchUnsignedCommit(size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.SetBytes(payloadLen(b, c))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := signObject(signer, c); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSignTag(b *testing.B) {
	signer := noopSigner{}
	r := &Repository{}
	for _, size := range signBenchSizes {
		tag := benchUnsignedTag(size)
		b.Run(fmt.Sprintf("body=%d", size), func(b *testing.B) {
			b.SetBytes(payloadLen(b, tag))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := r.buildTagSignature(tag, signer); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
