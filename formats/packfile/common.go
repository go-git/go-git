package packfile

import (
	"fmt"
	"io"
)

type trackingReader struct {
	r io.Reader
	n int
}

func (t *trackingReader) Pos() int { return t.n }

func (t *trackingReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if err != nil {
		return 0, err
	}

	t.n += n

	return n, err
}

func (t *trackingReader) ReadByte() (c byte, err error) {
	var p [1]byte
	n, err := t.r.Read(p[:])
	if err != nil {
		return 0, err
	}

	if n > 1 {
		return 0, fmt.Errorf("read %d bytes, should have read just 1", n)
	}

	t.n += n // n is 1
	return p[0], nil
}
