package sideband

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

const benchmarkReadSize = 16

func BenchmarkDemuxer(b *testing.B) {
	cases := []struct {
		name        string
		payloadSize int
		packets     int
	}{
		{name: "small_100", payloadSize: 64, packets: 100},
		{name: "small_1000", payloadSize: 64, packets: 1000},
		{name: "medium_100", payloadSize: 4096, packets: 100},
		{name: "medium_1000", payloadSize: 4096, packets: 1000},
		{name: "large_100", payloadSize: pktline.MaxPayloadSize - 1, packets: 100},
		{name: "large_1000", payloadSize: pktline.MaxPayloadSize - 1, packets: 1000},
	}

	for _, tc := range cases {
		input, want := benchmarkDemuxerInput(b, tc.payloadSize, tc.packets)

		b.Run(tc.name, func(b *testing.B) {
			buf := make([]byte, benchmarkReadSize)
			b.ReportAllocs()
			b.SetBytes(want)

			for b.Loop() {
				d := NewDemuxer(Sideband64k, bytes.NewReader(input))
				n, err := io.CopyBuffer(io.Discard, d, buf)
				if err != nil {
					b.Fatal(err)
				}
				if n != want {
					b.Fatalf("read %d bytes, want %d", n, want)
				}
			}
		})
	}
}

func benchmarkDemuxerInput(tb testing.TB, payloadSize, packets int) ([]byte, int64) {
	tb.Helper()

	payload := bytes.Repeat([]byte{'a'}, payloadSize)
	var input bytes.Buffer
	muxer := NewMuxer(Sideband64k, &input)

	for range packets {
		if _, err := muxer.Write(payload); err != nil {
			tb.Fatal(err)
		}
	}

	if err := pktline.WriteFlush(&input); err != nil {
		tb.Fatal(err)
	}

	return input.Bytes(), int64(payloadSize * packets)
}
