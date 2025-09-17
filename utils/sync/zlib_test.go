package sync

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"
)

func TestGetAndPutZlibReader(t *testing.T) {
	// FIXME: All the tests using sync.Pool are flaky.
	// There is always a chance we don't get the object we want.
	// from sync.Pool:
	// // Any item stored in the Pool may be removed automatically at any time without
	// // notification. If the Pool holds the only reference when this happens, the
	// // item might be deallocated.
	_, err := GetZlibReader(bytes.NewReader(zlibInitBytes))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	dict := GetByteSlice()
	defer PutByteSlice(dict)
	z1 := &ZLibReader{dict: dict, reader: &FakeZLibReader{}}
	PutZlibReader(z1)

	z2, err := GetZlibReader(bytes.NewReader(zlibInitBytes))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	if z1 != z2 {
		t.Errorf("zlib reader was not reused")
		return
	}

	if !z2.reader.(*FakeZLibReader).wasReset {
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
	wasReset bool
}

func (f *FakeZLibReader) Reset(r io.Reader, dict []byte) error {
	f.wasReset = true
	return nil
}

func (f *FakeZLibReader) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (f *FakeZLibReader) Close() error {
	return nil
}
