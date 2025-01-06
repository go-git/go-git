package sideband

import (
	"bytes"
)

func (s *SidebandSuite) TestMuxerWrite() {
	buf := bytes.NewBuffer(nil)

	m := NewMuxer(Sideband, buf)

	n, err := m.Write(bytes.Repeat([]byte{'F'}, (MaxPackedSize-1)*2))
	s.NoError(err)
	s.Equal(1998, n)
	s.Equal(2008, buf.Len())
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
