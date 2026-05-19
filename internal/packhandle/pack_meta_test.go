package packhandle

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
)

func makePack(t *testing.T, version, count uint32, hash plumbing.Hash, extra int) []byte {
	t.Helper()
	hashBytes := hash.Bytes()
	buf := make([]byte, 12+extra+len(hashBytes))
	copy(buf[0:4], "PACK")
	binary.BigEndian.PutUint32(buf[4:8], version)
	binary.BigEndian.PutUint32(buf[8:12], count)
	copy(buf[len(buf)-len(hashBytes):], hashBytes)
	return buf
}

func TestParsePackMeta_HappyPath(t *testing.T) {
	t.Parallel()
	hash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	data := makePack(t, 2, 42, hash, 8)
	src := &memCloser{Reader: bytes.NewReader(data)}

	meta, err := parsePackMeta(src, int64(len(data)), hash)
	if err != nil {
		t.Fatalf("parsePackMeta: %v", err)
	}
	if meta.Version != 2 || meta.Count != 42 || meta.ID != hash {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestParsePackMeta_HashMismatch(t *testing.T) {
	t.Parallel()
	footerHash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	expectedHash := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	data := makePack(t, 2, 1, footerHash, 0)
	src := &memCloser{Reader: bytes.NewReader(data)}

	_, err := parsePackMeta(src, int64(len(data)), expectedHash)
	if err == nil || !strings.Contains(err.Error(), "does not match pinned hash") {
		t.Fatalf("err = %v, want hash-mismatch", err)
	}
}

func TestParsePackMeta_BadMagic(t *testing.T) {
	t.Parallel()
	hash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	data := makePack(t, 2, 1, hash, 0)
	data[0] = 'N' // corrupt magic
	src := &memCloser{Reader: bytes.NewReader(data)}

	_, err := parsePackMeta(src, int64(len(data)), hash)
	if err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("err = %v, want magic-mismatch", err)
	}
}

func TestParsePackMeta_TooSmall(t *testing.T) {
	t.Parallel()
	hash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	data := []byte{'P', 'A', 'C', 'K'}
	src := &memCloser{Reader: bytes.NewReader(data)}

	_, err := parsePackMeta(src, int64(len(data)), hash)
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("err = %v, want too-small", err)
	}
}

func TestParsePackMeta_RejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	// 12-byte header with PACK magic, version=99, count=0.
	header := make([]byte, 12)
	copy(header[0:4], []byte("PACK"))
	binary.BigEndian.PutUint32(header[4:8], 99)
	binary.BigEndian.PutUint32(header[8:12], 0)

	// 20-byte SHA-1 footer of zeros.
	footer := make([]byte, 20)
	body := slices.Concat(header, footer)

	// Compute the footer hash from the footer bytes so parsePackMeta's
	// footer-hash check would pass, isolating the version-validation
	// path as the cause of any rejection.
	var packHash plumbing.Hash
	packHash.ResetBySize(20)
	_, _ = packHash.Write(footer)

	_, err := parsePackMeta(&memCloser{Reader: bytes.NewReader(body)}, int64(len(body)), packHash)
	if err == nil {
		t.Fatal("expected error for unsupported version, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported pack version") {
		t.Errorf("expected unsupported-version error, got: %v", err)
	}
}
