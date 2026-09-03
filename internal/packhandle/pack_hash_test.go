package packhandle

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
)

func packWithFooter(hash plumbing.Hash, extra int) []byte {
	data := make([]byte, 12+extra+hash.Size())
	copy(data[len(data)-hash.Size():], hash.Bytes())
	return data
}

func TestValidatePackHash(t *testing.T) {
	t.Parallel()

	for _, hashText := range []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	} {
		hash := plumbing.NewHash(hashText)
		data := packWithFooter(hash, 8)
		src := &memCloser{Reader: bytes.NewReader(data)}
		if err := validatePackHash(src, int64(len(data)), hash); err != nil {
			t.Fatalf("validatePackHash(%d-byte hash): %v", hash.Size(), err)
		}
	}
}

func TestValidatePackHashMismatch(t *testing.T) {
	t.Parallel()

	footerHash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	expectedHash := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	data := packWithFooter(footerHash, 0)
	err := validatePackHash(&memCloser{Reader: bytes.NewReader(data)}, int64(len(data)), expectedHash)
	if err == nil || !strings.Contains(err.Error(), "does not match pinned hash") {
		t.Fatalf("err = %v, want hash mismatch", err)
	}
}

func TestValidatePackHashRejectsSmallPack(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	data := make([]byte, 12+hash.Size()-1)
	err := validatePackHash(&memCloser{Reader: bytes.NewReader(data)}, int64(len(data)), hash)
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("err = %v, want too-small error", err)
	}
}
