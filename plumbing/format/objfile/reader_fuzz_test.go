package objfile

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

func FuzzReader(f *testing.F) {
	// Loose objects are fully zlib-compressed: the header
	// "<type> <size>\x00<body>" lives inside the zlib stream.
	addSeed := func(payload []byte) {
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		_, _ = w.Write(payload)
		_ = w.Close()
		f.Add(buf.Bytes())
	}
	addSeed([]byte("blob 5\x00hello"))
	addSeed([]byte("tree 0\x00"))
	addSeed([]byte("commit 0\x00"))
	addSeed([]byte("tag 0\x00"))
	// Header longer than the MAX_HEADER_LEN-equivalent cap, so the reader
	// must refuse it rather than draining the inflated stream.
	addSeed(bytes.Repeat([]byte{'b'}, 1024))
	addSeed(append([]byte("blob "), bytes.Repeat([]byte{'0'}, 1024)...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := NewReader(bytes.NewReader(data), format.SHA1)
		if err != nil {
			return
		}
		defer r.Close()
		_, size, err := r.Header()
		if err != nil {
			return
		}
		n, err := io.Copy(io.Discard, r)
		if n > size {
			t.Fatalf("Read %d bytes, more than the declared %d", n, size)
		}
		if err == nil && n != size {
			t.Fatalf("Read %d bytes without error, want the declared %d", n, size)
		}
		_ = r.Hash()
	})
}
