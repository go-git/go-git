package pktline_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// pkt builds a data pkt-line with the given payload. Payload must fit in
// MaxPayloadSize bytes.
func pkt(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := pktline.Write(&buf, payload); err != nil {
		t.Fatalf("pkt: %v", err)
	}
	return buf.Bytes()
}

// sbPkt builds a sideband data pkt-line on the given band.
func sbPkt(t *testing.T, band byte, payload []byte) []byte {
	t.Helper()
	return pkt(t, append([]byte{band}, payload...))
}

func TestSidebandScanner_BandData(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandData, []byte("hello ")))
	src.Write(sbPkt(t, pktline.BandData, []byte("world")))

	var progress bytes.Buffer
	s := pktline.NewSidebandScanner(&src, &progress, pktline.MaxSize)

	var got []byte
	for s.Scan() {
		got = append(got, s.Bytes()...)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("data = %q", got)
	}
	if progress.Len() != 0 {
		t.Fatalf("unexpected progress: %q", progress.String())
	}
}

func TestSidebandScanner_ProgressLineBuffering(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	// "Counting" split across two band-2 packets; full line only completes
	// at the trailing '\n' in the second packet.
	src.Write(sbPkt(t, pktline.BandProgress, []byte("Counti")))
	src.Write(sbPkt(t, pktline.BandProgress, []byte("ng objects\n")))
	// Two CR-delimited progress fragments in a single band-2 packet.
	src.Write(sbPkt(t, pktline.BandProgress, []byte("Resolving deltas: 50%\rResolving deltas: 100%\r")))
	src.Write(sbPkt(t, pktline.BandData, []byte("pack")))

	var progress bytes.Buffer
	s := pktline.NewSidebandScanner(&src, &progress, pktline.MaxSize)

	var data []byte
	for s.Scan() {
		data = append(data, s.Bytes()...)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if string(data) != "pack" {
		t.Fatalf("data = %q", data)
	}
	want := "remote: Counting objects\nremote: Resolving deltas: 50%\rremote: Resolving deltas: 100%\r"
	if progress.String() != want {
		t.Fatalf("progress = %q\nwant      %q", progress.String(), want)
	}
}

func TestSidebandScanner_ResidualProgressFlushedOnEOF(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandProgress, []byte("trailing")))
	// stream ends without a newline.

	var progress bytes.Buffer
	s := pktline.NewSidebandScanner(&src, &progress, pktline.MaxSize)
	for s.Scan() {
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if progress.String() != "remote: trailing" {
		t.Fatalf("progress = %q", progress.String())
	}
}

func TestSidebandScanner_BandError(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandData, []byte("partial")))
	src.Write(sbPkt(t, pktline.BandError, []byte("server exploded")))
	src.Write(sbPkt(t, pktline.BandData, []byte("never seen")))

	s := pktline.NewSidebandScanner(&src, nil, pktline.MaxSize)
	var data []byte
	for s.Scan() {
		data = append(data, s.Bytes()...)
	}
	if string(data) != "partial" {
		t.Fatalf("data = %q", data)
	}
	var se *pktline.SidebandError
	if !errors.As(s.Err(), &se) {
		t.Fatalf("Err = %v, want *SidebandError", s.Err())
	}
	if se.Msg != "server exploded" {
		t.Fatalf("Msg = %q", se.Msg)
	}
}

func TestSidebandScanner_ResidualProgressFlushedOnBand3(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandProgress, []byte("midline")))
	src.Write(sbPkt(t, pktline.BandError, []byte("boom")))

	var progress bytes.Buffer
	s := pktline.NewSidebandScanner(&src, &progress, pktline.MaxSize)
	for s.Scan() {
	}
	var se *pktline.SidebandError
	if !errors.As(s.Err(), &se) {
		t.Fatalf("Err = %v", s.Err())
	}
	if progress.String() != "remote: midline" {
		t.Fatalf("progress = %q", progress.String())
	}
}

func TestSidebandScanner_MaxPacketExceeded(t *testing.T) {
	t.Parallel()
	// One byte over DefaultSize on the wire.
	payload := bytes.Repeat([]byte("a"), pktline.DefaultPayloadSize) // 996 bytes; +4 header + 1 band byte = 1001
	src := sbPkt(t, pktline.BandData, payload)

	s := pktline.NewSidebandScanner(bytes.NewReader(src), nil, pktline.DefaultSize)
	if s.Scan() {
		t.Fatalf("Scan succeeded; want failure")
	}
	if !errors.Is(s.Err(), pktline.ErrMaxPacketExceeded) {
		t.Fatalf("Err = %v, want ErrMaxPacketExceeded", s.Err())
	}
}

func TestSidebandScanner_MaxPacketAllowedAtBoundary(t *testing.T) {
	t.Parallel()
	// Exactly at DefaultSize: 4-byte header + 1-byte band + 995-byte payload = 1000.
	payload := bytes.Repeat([]byte("a"), pktline.DefaultPayloadSize-1)
	src := sbPkt(t, pktline.BandData, payload)

	s := pktline.NewSidebandScanner(bytes.NewReader(src), nil, pktline.DefaultSize)
	if !s.Scan() {
		t.Fatalf("Scan failed: %v", s.Err())
	}
	if !bytes.Equal(s.Bytes(), payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestSidebandScanner_FlushDelimResponseEnd(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandData, []byte("x")))
	if err := pktline.WriteDelim(&src); err != nil {
		t.Fatal(err)
	}
	src.Write(sbPkt(t, pktline.BandData, []byte("y")))
	if err := pktline.WriteFlush(&src); err != nil {
		t.Fatal(err)
	}

	s := pktline.NewSidebandScanner(&src, nil, pktline.MaxSize)
	var lens []int
	var got []byte
	for s.Scan() {
		lens = append(lens, s.Len())
		got = append(got, s.Bytes()...)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if string(got) != "xy" {
		t.Fatalf("data = %q", got)
	}
	wantLens := []int{LenForData(t, 1), pktline.Delim, LenForData(t, 1), pktline.Flush}
	if !equalInts(lens, wantLens) {
		t.Fatalf("lens = %v, want %v", lens, wantLens)
	}
}

func TestSidebandScanner_UnknownBand(t *testing.T) {
	t.Parallel()
	src := sbPkt(t, 7, []byte("nope"))
	s := pktline.NewSidebandScanner(bytes.NewReader(src), nil, pktline.MaxSize)
	if s.Scan() {
		t.Fatalf("Scan succeeded; want failure")
	}
	if s.Err() == nil || !strings.Contains(s.Err().Error(), "unknown sideband channel") {
		t.Fatalf("Err = %v", s.Err())
	}
}

func TestSidebandScanner_NilProgress(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	src.Write(sbPkt(t, pktline.BandProgress, []byte("ignored\n")))
	src.Write(sbPkt(t, pktline.BandData, []byte("d")))

	s := pktline.NewSidebandScanner(&src, nil, pktline.MaxSize)
	for s.Scan() {
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
}

// TestSidebandScanner_EmptyPacketMissingBandByte covers the regression
// where a zero-length sideband data packet (an empty "0004" pkt-line,
// carrying no band designator) was previously accepted silently. Upstream
// Git's demultiplex_sideband treats this as
// "protocol error: missing sideband designator".
func TestSidebandScanner_EmptyPacketMissingBandByte(t *testing.T) {
	t.Parallel()
	var src bytes.Buffer
	if _, err := pktline.Write(&src, nil); err != nil { // empty data packet ("0004")
		t.Fatal(err)
	}

	s := pktline.NewSidebandScanner(&src, nil, pktline.MaxSize)
	if s.Scan() {
		t.Fatalf("Scan succeeded; want failure")
	}
	if s.Err() == nil || !strings.Contains(s.Err().Error(), "missing band byte") {
		t.Fatalf("Err = %v, want missing band byte error", s.Err())
	}
}

func TestSidebandScanner_PlainScannerUnchanged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if _, err := pktline.Write(&buf, []byte("plain")); err != nil {
		t.Fatal(err)
	}
	s := pktline.NewScanner(&buf)
	if !s.Scan() {
		t.Fatalf("Scan failed: %v", s.Err())
	}
	if string(s.Bytes()) != "plain" {
		t.Fatalf("payload = %q", s.Bytes())
	}
}

// LenForData returns the on-wire length of a sideband data pkt-line with
// the given non-band payload length.
func LenForData(t *testing.T, payloadLen int) int {
	t.Helper()
	return pktline.LenSize + 1 + payloadLen
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
