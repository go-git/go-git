package idxfile

import (
	"bytes"
	"io/fs"
	"time"
)

// FromBytes wraps an in-memory idx blob as an [Input]. It is only
// available in tests so production code is steered toward passing
// the underlying file directly.
func FromBytes(b []byte) Input {
	return bytesInput{Reader: bytes.NewReader(b), size: int64(len(b))}
}

type bytesInput struct {
	*bytes.Reader
	size int64
}

func (b bytesInput) Stat() (fs.FileInfo, error) {
	return bytesInputInfo(b.size), nil
}

type bytesInputInfo int64

func (i bytesInputInfo) Name() string       { return "" }
func (i bytesInputInfo) Size() int64        { return int64(i) }
func (i bytesInputInfo) Mode() fs.FileMode  { return 0 }
func (i bytesInputInfo) ModTime() time.Time { return time.Time{} }
func (i bytesInputInfo) IsDir() bool        { return false }
func (i bytesInputInfo) Sys() any           { return nil }
