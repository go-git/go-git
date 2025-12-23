package readerat

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	ErrObjectNotFound  = errors.New("object not found")
	ErrOffsetNotFound  = errors.New("offset not found in packfile")
	ErrHashParseFailed = errors.New("failed to parse hash")
	ErrCorruptedIdx    = errors.New("corrupted idx file")
	ErrNilFile         = errors.New("cannot open file: file is nil")
)

// validateHeader validates the signature and version of a file using ReadAt.
func validateHeader(r io.ReaderAt, sig []byte, sv uint32, minLen int64, fileSize int64) error {
	if minLen > fileSize {
		return io.EOF
	}

	header := make([]byte, len(sig)+4)
	n, err := r.ReadAt(header, 0)
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}
	if n != len(header) {
		return fmt.Errorf("short read: got %d bytes, expected %d", n, len(header))
	}

	if !bytes.Equal(sig, header[:len(sig)]) {
		return fmt.Errorf("signature mismatch")
	}

	v := binary.BigEndian.Uint32(header[len(sig) : len(sig)+4])
	if sv != v {
		return fmt.Errorf("unsupported version: %d", v)
	}

	return nil
}
