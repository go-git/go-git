package sync

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"
)

func TestGetAndPutZlibReader(t *testing.T) {
	_, err := GetZlibReader(bytes.NewReader(zlibInitBytes))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	dict := &[]byte{}
	reader := FakeZLibReader{}
	PutZlibReader(ZLibReader{dict: dict, Reader: &reader})

	if !reader.wasClosed {
		t.Errorf("reader was not closed")
	}

	z2, err := GetZlibReader(bytes.NewReader(zlibInitBytes))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if dict != z2.dict {
		t.Errorf("zlib dictionary was not reused")
	}

	if &reader != z2.Reader {
		t.Errorf("zlib reader was not reused")
	}

	if !reader.wasReset {
		t.Errorf("reader was not reset")
	}
}

func TestGetAndPutZlibWriter(t *testing.T) {
	w := GetZlibWriter(nil)
	if w == nil {
		t.Errorf("nil was not expected")
	}

	newW := zlib.NewWriter(nil)
	PutZlibWriter(newW)

	w2 := GetZlibWriter(nil)
	if w2 != newW {
		t.Errorf("zlib writer was not reused")
	}
}

type FakeZLibReader struct {
	wasClosed bool
	wasReset  bool
}

func (f *FakeZLibReader) Reset(r io.Reader, dict []byte) error {
	f.wasReset = true
	return nil
}

func (f *FakeZLibReader) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (f *FakeZLibReader) Close() error {
	f.wasClosed = true
	return nil
}
