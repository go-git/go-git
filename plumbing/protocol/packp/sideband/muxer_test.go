package sideband

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// sidebandTypes pairs each sideband type with the maximum pkt-line size it
// permits, counting the 4-byte length prefix.
var sidebandTypes = []struct {
	name string
	t    Type
	max  int
}{
	{"side-band", Sideband, MaxPackedSize},
	{"side-band-64k", Sideband64k, MaxPackedSize64k},
}

func (s *SidebandSuite) TestMuxerWrite() {
	buf := bytes.NewBuffer(nil)

	m := NewMuxer(Sideband, buf)

	n, err := m.Write(bytes.Repeat([]byte{'F'}, (MaxPackedSize-1)*2))
	s.NoError(err)
	s.Equal(1998, n)
	s.Equal(2013, buf.Len())
}

// TestMuxerChunksWithinPacketSizeLimit checks the invariant the Demuxer
// enforces on the wire: no pkt-line the Muxer emits exceeds the maximum packet
// size of its sideband type. Both the length prefix and the channel byte come
// out of that budget, so a chunk may carry at most max-5 bytes of payload.
//
// Chunking used to leave room for the channel byte only. side-band therefore
// emitted 1004-byte packets, which the Demuxer rejects with
// ErrMaxPackedExceeded, and side-band-64k chunks crossed
// pktline.MaxPayloadSize outright once a single Write reached 65516 bytes.
func (s *SidebandSuite) TestMuxerChunksWithinPacketSizeLimit() {
	for _, tc := range sidebandTypes {
		s.Run(tc.name, func() {
			// Two saturated chunks and a short remainder.
			payload := bytes.Repeat([]byte{'F'}, tc.max*2+7)

			var buf bytes.Buffer
			n, err := NewMuxer(tc.t, &buf).Write(payload)
			s.Require().NoError(err)
			s.Equal(len(payload), n)

			sc := pktline.NewScanner(bytes.NewReader(buf.Bytes()))
			var pkts int
			for sc.Scan() {
				pkts++
				s.Require().LessOrEqual(sc.Len(), tc.max,
					"packet %d of %d exceeds the limit", pkts, tc.max)
			}
			s.Require().NoError(sc.Err())
			s.Greater(pkts, 2, "expected the payload to be chunked")

			// The Demuxer applies the same limit, so reading the stream back
			// is the wire-level check.
			got, err := io.ReadAll(NewDemuxer(tc.t, bytes.NewReader(buf.Bytes())))
			s.Require().NoError(err)
			s.Len(got, len(payload))
			s.True(bytes.Equal(payload, got), "payload did not survive the round-trip")
		})
	}
}

// TestMuxerWriteFillsSinglePacket pins the boundary: the largest payload a
// single packet can carry must not spill into a second one, and must fill the
// packet exactly.
func (s *SidebandSuite) TestMuxerWriteFillsSinglePacket() {
	for _, tc := range sidebandTypes {
		s.Run(tc.name, func() {
			var buf bytes.Buffer
			payload := bytes.Repeat([]byte{'F'}, tc.max-pktline.LenSize-chLen)

			n, err := NewMuxer(tc.t, &buf).Write(payload)
			s.Require().NoError(err)
			s.Equal(len(payload), n)
			s.Equal(tc.max, buf.Len())

			sc := pktline.NewScanner(bytes.NewReader(buf.Bytes()))
			s.Require().True(sc.Scan())
			s.Equal(tc.max, sc.Len())
			s.False(sc.Scan(), "payload spilled into a second packet")
			s.Require().NoError(sc.Err())
		})
	}
}

func (s *SidebandSuite) TestMuxerWriteChannelMultipleChannels() {
	buf := bytes.NewBuffer(nil)

	m := NewMuxer(Sideband, buf)

	n, err := m.WriteChannel(PackData, bytes.Repeat([]byte{'D'}, 4))
	s.NoError(err)
	s.Equal(4, n)

	n, err = m.WriteChannel(ProgressMessage, bytes.Repeat([]byte{'P'}, 4))
	s.NoError(err)
	s.Equal(4, n)

	n, err = m.WriteChannel(PackData, bytes.Repeat([]byte{'D'}, 4))
	s.NoError(err)
	s.Equal(4, n)

	s.Equal(27, buf.Len())
	s.Equal("0009\x01DDDD0009\x02PPPP0009\x01DDDD", buf.String())
}

func (s *SidebandSuite) TestMuxerReusedBufferKeepsEncoding() {
	for _, tc := range sidebandTypes {
		s.Run(tc.name, func() {
			chunk := tc.max - pktline.LenSize - chLen
			writes := []struct {
				ch   Channel
				size int
			}{
				{PackData, 2*chunk + 7},
				{ProgressMessage, 3},
				{PackData, chunk},
				{ErrorMessage, 1},
				{PackData, chunk + 1},
			}

			var got, want bytes.Buffer
			m := NewMuxer(tc.t, &got)
			for i, w := range writes {
				payload := bytes.Repeat([]byte{byte('a' + i)}, w.size)
				n, err := m.WriteChannel(w.ch, payload)
				s.Require().NoError(err)
				s.Equal(w.size, n)

				for p := payload; len(p) > 0; {
					sz := min(len(p), chunk)
					_, err := pktline.Write(&want, append([]byte{byte(w.ch)}, p[:sz]...))
					s.Require().NoError(err)
					p = p[sz:]
				}
			}
			s.Equal(want.Bytes(), got.Bytes())
		})
	}
}

func (s *SidebandSuite) TestMuxerConcurrentWritesKeepPackets() {
	for _, tc := range sidebandTypes {
		s.Run(tc.name, func() {
			var buf bytes.Buffer
			m := NewMuxer(tc.t, &buf)
			pack := make([]byte, 8*tc.max)
			for i := range pack {
				pack[i] = byte(i)
			}
			const writers, messages = 4, 50

			var wg sync.WaitGroup
			wg.Go(func() {
				_, err := m.Write(pack)
				s.NoError(err)
			})
			for w := range writers {
				wg.Go(func() {
					for i := range messages {
						_, err := m.WriteChannel(ProgressMessage, fmt.Appendf(nil, "w%d-%03d\n", w, i))
						s.NoError(err)
					}
				})
			}
			wg.Wait()

			var progress bytes.Buffer
			d := NewDemuxer(tc.t, bytes.NewReader(buf.Bytes()))
			d.Progress = &progress
			got, err := io.ReadAll(d)
			s.Require().NoError(err)
			s.True(bytes.Equal(pack, got), "pack data did not survive concurrent progress writes")

			lines := strings.Split(strings.TrimSuffix(progress.String(), "\n"), "\n")
			s.Require().Len(lines, writers*messages)
			next := make([]int, writers)
			for _, line := range lines {
				var w, i int
				_, err := fmt.Sscanf(line, "w%d-%03d", &w, &i)
				s.Require().NoError(err, "progress line %q", line)
				s.Equal(next[w], i, "writer %d", w)
				next[w]++
			}
		})
	}
}

func TestMuxerWriteDoesNotAllocatePerPayload(t *testing.T) { //nolint:paralleltest // testing.AllocsPerRun forbids parallel
	const packets = 16
	m := NewMuxer(Sideband64k, io.Discard)
	payload := bytes.Repeat([]byte{'F'}, packets*m.max)
	var err error
	allocs := testing.AllocsPerRun(10, func() {
		_, err = m.Write(payload)
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocs > packets {
		t.Errorf("Write of %d packets made %.0f allocations, want at most %d", packets, allocs, packets)
	}
}

func BenchmarkMuxerWrite(b *testing.B) {
	payload := bytes.Repeat([]byte{'F'}, 1<<20)
	for _, tc := range sidebandTypes {
		b.Run(tc.name, func(b *testing.B) {
			m := NewMuxer(tc.t, io.Discard)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := m.Write(payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
