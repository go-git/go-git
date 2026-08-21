package sideband

import (
	"bytes"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func (s *SidebandSuite) TestMuxerWrite() {
	buf := bytes.NewBuffer(nil)

	m := NewMuxer(Sideband, buf)

	n, err := m.Write(bytes.Repeat([]byte{'F'}, (MaxPackedSize-1)*2))
	s.NoError(err)
	s.Equal(1998, n)
	s.Equal(2013, buf.Len())
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

// budgetWriter accepts at most budget bytes in total and then fails, reporting
// how much of the final write it took as the io.Writer contract requires.
type budgetWriter struct {
	budget int
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	if w.budget <= 0 {
		return 0, io.ErrShortWrite
	}
	if len(p) > w.budget {
		n := w.budget
		w.budget = 0
		return n, io.ErrShortWrite
	}
	w.budget -= len(p)
	return len(p), nil
}

// TestMuxerWriteFailureReportsBytesWritten checks that a failed write reports
// only the bytes of p that reached the underlying writer. Every pkt-line leads
// with a 4-byte length prefix and a channel byte, neither of which comes from
// p, so a write that dies on the prefix has consumed nothing.
func (s *SidebandSuite) TestMuxerWriteFailureReportsBytesWritten() {
	const payloadPerPacket = MaxPackedSize - pktline.LenSize - chLen

	for _, tc := range []struct {
		name   string
		budget int
		want   int
	}{
		{"nothing accepted", 0, 0},
		{"dies mid prefix", 2, 0},
		{"prefix only", pktline.LenSize, 0},
		{"prefix and channel byte", pktline.LenSize + chLen, 0},
		{"ten payload bytes", pktline.LenSize + chLen + 10, 10},
		{
			// A full first packet, then a second that dies ten payload bytes in.
			"across a chunk boundary",
			MaxPackedSize + pktline.LenSize + chLen + 10,
			payloadPerPacket + 10,
		},
	} {
		s.Run(tc.name, func() {
			w := &budgetWriter{budget: tc.budget}

			n, err := NewMuxer(Sideband, w).Write(bytes.Repeat([]byte{'F'}, payloadPerPacket*2))
			s.ErrorIs(err, io.ErrShortWrite)
			s.Equal(tc.want, n)
		})
	}
}
