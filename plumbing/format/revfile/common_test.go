package revfile

import (
	"bytes"
	"io/fs"
	"time"
)

// mockRevFile wraps a bytes.Reader to satisfy the RevFile interface for testing.
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
