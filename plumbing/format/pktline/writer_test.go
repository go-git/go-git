package pktline_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func TestWriter_Plain_SmallPayload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewWriter(&buf)
	n, err := w.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v)", n, err)
	}
	if buf.String() != "0009hello" {
		t.Fatalf("buf = %q", buf.String())
	}
}

func TestWriter_Plain_ChunksLargePayload(t *testing.T) {
	t.Parallel()
	// Payload larger than MaxPayloadSize must be split.
	payload := bytes.Repeat([]byte("a"), pktline.MaxPayloadSize+10)
	var buf bytes.Buffer
	w := pktline.NewWriter(&buf)
	n, err := w.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("n = %d, want %d", n, len(payload))
	}
	r := pktline.NewReader(&buf)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestWriter_Plain_EmptyPayload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewWriter(&buf)
	if _, err := w.Write(nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "0004" {
		t.Fatalf("buf = %q", buf.String())
	}
}

func TestWriter_Plain_ProgressIsNoOp(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := pktline.NewWriter(&buf)
	n, err := w.WriteProgress([]byte("x"))
	if err != nil || n != 1 {
		t.Fatalf("WriteProgress = (%d, %v)", n, err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf = %q", buf.String())
	}
}

func TestWriter_Sideband_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		maxSize int
	}{
		{"DefaultSize", pktline.DefaultSize},
		{"MaxSize", pktline.MaxSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := bytes.Repeat([]byte("p"), tc.maxSize*2+17)
			var buf bytes.Buffer
			w := pktline.NewSidebandWriter(&buf, tc.maxSize)
			if _, err := w.Write(payload); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if _, err := w.WriteProgress([]byte("counting\n")); err != nil {
				t.Fatalf("WriteProgress: %v", err)
			}
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}

			s := pktline.NewSidebandScanner(&buf, io.Discard, tc.maxSize)
			var got []byte
			for s.Scan() {
				if s.Len() == pktline.Flush {
					continue
				}
				got = append(got, s.Bytes()...)
			}
			if err := s.Err(); err != nil {
				t.Fatalf("Err: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("round-trip mismatch (got %d bytes, want %d)", len(got), len(payload))
			}
		})
	}
}

func TestWriter_Sideband_NoPktLineExceedsMax(t *testing.T) {
	t.Parallel()
	maxSize := pktline.DefaultSize
	payload := bytes.Repeat([]byte("x"), maxSize*3)
	var buf bytes.Buffer
	w := pktline.NewSidebandWriter(&buf, maxSize)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	s := pktline.NewScanner(&buf)
	for s.Scan() {
		if s.Len() > maxSize {
			t.Fatalf("pkt-line length %d exceeds maxSize %d", s.Len(), maxSize)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
}

func TestWriteSideband_BandBytePrepended(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if _, err := pktline.WriteSideband(&buf, pktline.BandError, []byte("oops"), pktline.MaxSize); err != nil {
		t.Fatalf("WriteSideband: %v", err)
	}
	// 4-byte length header + 1-byte band + 4-byte payload = 9 bytes => 0009.
	if buf.String() != "0009\x03oops" {
		t.Fatalf("buf = %q", buf.String())
	}
}

func TestWriteSideband_EmptyPayload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	n, err := pktline.WriteSideband(&buf, pktline.BandData, nil, pktline.MaxSize)
	if err != nil || n != 0 {
		t.Fatalf("WriteSideband = (%d, %v)", n, err)
	}
	// 4-byte length header + 1-byte band = 5 bytes => 0005.
	if buf.String() != "0005\x01" {
		t.Fatalf("buf = %q", buf.String())
	}
}

func TestWriter_NilUnderlying(t *testing.T) {
	t.Parallel()
	w := pktline.NewWriter(nil)
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatalf("Write on nil writer succeeded")
	}
}

func TestWriteSideband_DefaultSizeWireBound(t *testing.T) {
	t.Parallel()
	// Regression: legacy side-band pkt-lines must be strictly <= 1000
	// bytes total. Sideband Muxer used to emit 1004-byte pkt-lines.
	payload := bytes.Repeat([]byte("z"), 5000)
	var buf bytes.Buffer
	if _, err := pktline.WriteSideband(&buf, pktline.BandData, payload, pktline.DefaultSize); err != nil {
		t.Fatal(err)
	}
	rest := buf.String()
	for len(rest) >= 4 {
		var l int
		for i := range 4 {
			c := rest[i]
			var v int
			switch {
			case c >= '0' && c <= '9':
				v = int(c - '0')
			case c >= 'a' && c <= 'f':
				v = int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v = int(c-'A') + 10
			default:
				t.Fatalf("bad hex %q", rest[:4])
			}
			l = l*16 + v
		}
		if l > pktline.DefaultSize {
			t.Fatalf("pkt-line length %d exceeds DefaultSize %d", l, pktline.DefaultSize)
		}
		if l < 4 || l > len(rest) {
			t.Fatalf("bad length %d (remaining %d)", l, len(rest))
		}
		rest = rest[l:]
	}
	if rest != "" {
		t.Fatalf("trailing bytes: %d", len(rest))
	}
	// Sanity: confirm the buffer parses cleanly.
	s := pktline.NewScanner(strings.NewReader(buf.String()))
	for s.Scan() {
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan err: %v", err)
	}
}
