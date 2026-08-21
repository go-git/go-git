package sideband

import (
	"bytes"
	"io"

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
