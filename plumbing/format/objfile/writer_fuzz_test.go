package objfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// FuzzWriterReaderRoundTrip drives Writer with a fuzz-supplied object
// type and declared size, then parses the produced loose-object stream
// via Reader. Every Writer call that returns nil must produce a stream
// the Reader accepts. This catches future drift between the writer's
// MAX_HEADER_LEN cap and the reader's MAX_HEADER_LEN budget, and any
// other shape change that breaks read/write symmetry.
func FuzzWriterReaderRoundTrip(f *testing.F) {
	f.Add(byte(0), int64(0))
	f.Add(byte(1), int64(5))
	f.Add(byte(2), int64(1<<62))
	f.Add(byte(3), int64(-1))

	f.Fuzz(func(t *testing.T, typeIdx byte, size int64) {
		types := []plumbing.ObjectType{
			plumbing.BlobObject,
			plumbing.TreeObject,
			plumbing.CommitObject,
			plumbing.TagObject,
		}
		ot := types[int(typeIdx)%len(types)]

		var buf bytes.Buffer
		w := NewWriter(&buf, format.SHA1)
		if err := w.WriteHeader(ot, size); err != nil {
			// Legitimate Writer rejections: ErrNegativeSize,
			// ErrHeaderTooLong, plumbing.ErrInvalidType.
			return
		}

		// Body: write the declared size up to a safety cap. Fuzz inputs
		// can legitimately declare 2^62 bytes; the header is what we are
		// here to round-trip, not the payload.
		const bodyCap = 1 << 14
		body := size
		if body < 0 || body > bodyCap {
			body = 0
		}
		if body > 0 {
			if _, err := w.Write(bytes.Repeat([]byte{0xab}, int(body))); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		r, err := NewReader(&buf, format.SHA1)
		if err != nil {
			t.Fatalf("NewReader: %v", err)
		}
		defer func() { _ = r.Close() }()

		gotType, gotSize, err := r.Header()
		if err != nil {
			t.Fatalf("Header: %v", err)
		}
		if gotType != ot || gotSize != size {
			t.Fatalf("Header = %v %d, want %v %d", gotType, gotSize, ot, size)
		}
		if body != size {
			return
		}
		n, err := io.Copy(io.Discard, r)
		if err != nil || n != size {
			t.Fatalf("Read %d bytes, err %v, want %d bytes", n, err, size)
		}
	})
}
