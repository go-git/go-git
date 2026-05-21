package pktline_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func TestReader_Plain_Concatenates(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	for _, s := range []string{"foo", "bar", "baz"} {
		if _, err := pktline.WriteString(&buf, s); err != nil {
			t.Fatal(err)
		}
	}
	r := pktline.NewReader(&buf)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "foobarbaz" {
		t.Fatalf("got %q", got)
	}
}

func TestReader_Plain_EOFOnFlush(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if _, err := pktline.WriteString(&buf, "first"); err != nil {
		t.Fatal(err)
	}
	if err := pktline.WriteFlush(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := pktline.WriteString(&buf, "after-flush"); err != nil {
		t.Fatal(err)
	}
	r := pktline.NewReader(&buf)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("got %q", got)
	}
}

func TestReader_Plain_EOFOnDelim(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if _, err := pktline.WriteString(&buf, "x"); err != nil {
		t.Fatal(err)
	}
	if err := pktline.WriteDelim(&buf); err != nil {
		t.Fatal(err)
	}
	r := pktline.NewReader(&buf)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestReader_ZeroLength(t *testing.T) {
	t.Parallel()
	r := pktline.NewReader(bytes.NewReader(nil))
	n, err := r.Read(nil)
	if n != 0 || err != nil {
		t.Fatalf("Read(nil) = (%d, %v)", n, err)
	}
}

func TestReader_PartialBufferSpansReads(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	if _, err := pktline.WriteString(&src, "abcdefghij"); err != nil {
		t.Fatal(err)
	}
	r := pktline.NewReader(&src)
	got, err := io.ReadAll(readByOneByte{r})
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "abcdefghij" {
		t.Fatalf("got %q", got)
	}
}

func TestReader_SkipsEmptyPackets(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	if _, err := pktline.Write(&src, nil); err != nil { // empty data packet (0004)
		t.Fatal(err)
	}
	if _, err := pktline.WriteString(&src, "after-empty"); err != nil {
		t.Fatal(err)
	}
	r := pktline.NewReader(&src)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "after-empty" {
		t.Fatalf("got %q", got)
	}
}

func TestSidebandReader_Concatenates(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandData, []byte("PACK")))
	src.Write(sbPkt(t, pktline.BandProgress, []byte("counting\n")))
	src.Write(sbPkt(t, pktline.BandData, []byte("data")))
	if err := pktline.WriteFlush(&src); err != nil {
		t.Fatal(err)
	}

	var progress bytes.Buffer
	r := pktline.NewSidebandReader(&src, &progress, pktline.MaxSize)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "PACKdata" {
		t.Fatalf("data = %q", got)
	}
	if progress.String() != "counting\n" {
		t.Fatalf("progress = %q", progress.String())
	}
}

func TestSidebandReader_BandErrorSurfaces(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandData, []byte("partial")))
	src.Write(sbPkt(t, pktline.BandError, []byte("boom")))

	r := pktline.NewSidebandReader(&src, nil, pktline.MaxSize)
	got, err := io.ReadAll(r)
	if string(got) != "partial" {
		t.Fatalf("data = %q", got)
	}
	var se *pktline.SidebandError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *SidebandError", err)
	}
	if se.Msg != "boom" {
		t.Fatalf("Msg = %q", se.Msg)
	}
}

func TestSidebandReader_NilProgress(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandProgress, []byte("ignored\n")))
	src.Write(sbPkt(t, pktline.BandData, []byte("ok")))

	r := pktline.NewSidebandReader(&src, nil, pktline.MaxSize)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("data = %q", got)
	}
}

type readByOneByte struct{ r io.Reader }

func (r readByOneByte) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var b [1]byte
	n, err := r.r.Read(b[:])
	if n > 0 {
		p[0] = b[0]
	}
	return n, err
}

func TestNewReaderFromScanner_SharedScannerInterleaved(t *testing.T) {
	t.Parallel()
	// Wire: two header packets, one flush, payload packets, flush.
	// Caller inspects the header packets via Scanner, then switches to
	// Reader to drain the payload as a byte stream.
	var buf bytes.Buffer
	if _, err := pktline.WriteString(&buf, "ACK 0000\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := pktline.WriteString(&buf, "NAK\n"); err != nil {
		t.Fatal(err)
	}
	if err := pktline.WriteFlush(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := pktline.WriteString(&buf, "PACK"); err != nil {
		t.Fatal(err)
	}
	if _, err := pktline.WriteString(&buf, "data"); err != nil {
		t.Fatal(err)
	}
	if err := pktline.WriteFlush(&buf); err != nil {
		t.Fatal(err)
	}

	s := pktline.NewScanner(&buf)

	var headers []string
	for s.Scan() {
		if s.Len() == pktline.Flush {
			break
		}
		headers = append(headers, string(s.Bytes()))
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scanner err: %v", err)
	}
	if len(headers) != 2 || headers[0] != "ACK 0000\n" || headers[1] != "NAK\n" {
		t.Fatalf("headers = %q", headers)
	}

	r := pktline.NewReaderFromScanner(s)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "PACKdata" {
		t.Fatalf("payload = %q", got)
	}
}
