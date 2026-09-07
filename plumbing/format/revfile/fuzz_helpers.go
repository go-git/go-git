package revfile

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"time"
)

// The helpers in this file live outside a _test.go file on purpose. The
// OSS-Fuzz harness extracts each FuzzXxx target into a standalone non-test
// file and strips the rest of the package's _test.go files, so any helper a
// fuzz target references must be compiled into the package itself to remain
// visible at fuzz build time. They are otherwise only used by tests, which
// keeps them out of the public API while satisfying the unused-code linter
// (the in-package tests reference them). This mirrors the approach used by
// plumbing/format/idxfile/fuzz_helpers.go.

// mockRevFile wraps a bytes.Reader to satisfy the RevFile interface for
// testing and fuzzing.
type mockRevFile struct {
	*bytes.Reader
	size   int64
	closer func() error
}

func newMockRevFile(data []byte) *mockRevFile {
	return &mockRevFile{
		Reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}
}

func (m *mockRevFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{size: m.size}, nil
}

func (m *mockRevFile) Close() error {
	if m.closer != nil {
		return m.closer()
	}
	return nil
}

type mockFileInfo struct {
	size int64
}

func (m *mockFileInfo) Name() string       { return "test.rev" }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() fs.FileMode  { return 0o644 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() any           { return nil }

// buildMinimalRev constructs a minimal valid .rev file for the given number
// of objects and hash size. The entries are the identity mapping (already
// sorted by offset) and the trailing pack/rev checksums are zeroed. It is
// used to seed the fuzz corpus with inputs that reach the success path
// (header validation, iteration and lookups) rather than only the early
// validation failures. Mirrors plumbing/format/idxfile.buildMinimalRev.
func buildMinimalRev(count, hashSize int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{'R', 'I', 'D', 'X'})
	_ = binary.Write(&buf, binary.BigEndian, uint32(VersionSupported))

	hashID := sha1Hash
	if hashSize == 32 {
		hashID = sha256Hash
	}
	_ = binary.Write(&buf, binary.BigEndian, hashID)

	// Entries: identity mapping (already sorted by offset).
	for i := range count {
		_ = binary.Write(&buf, binary.BigEndian, uint32(i))
	}

	// Trailing pack checksum + rev checksum.
	buf.Write(make([]byte, hashSize*2))
	return buf.Bytes()
}
