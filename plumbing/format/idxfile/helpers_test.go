package idxfile_test

import (
	"bytes"
	"io/fs"
	"time"
)

// bytesInput wraps an in-memory idx blob with a Stat method so that
// the [Decoder] can probe its on-disk length and apply the size
// formula. The interface is unexported in [Decoder]; the type
// assertion succeeds on any value that exposes both
// [io.Reader] and `Stat() (fs.FileInfo, error)`.
type bytesInput struct {
	*bytes.Reader
	size int64
}

func fromBytes(b []byte) *bytesInput {
	return &bytesInput{Reader: bytes.NewReader(b), size: int64(len(b))}
}

func (b *bytesInput) Stat() (fs.FileInfo, error) {
	return bytesInputInfo(b.size), nil
}

type bytesInputInfo int64

func (i bytesInputInfo) Name() string       { return "" }
func (i bytesInputInfo) Size() int64        { return int64(i) }
func (i bytesInputInfo) Mode() fs.FileMode  { return 0 }
func (i bytesInputInfo) ModTime() time.Time { return time.Time{} }
func (i bytesInputInfo) IsDir() bool        { return false }
func (i bytesInputInfo) Sys() any           { return nil }
