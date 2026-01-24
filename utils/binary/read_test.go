package binary

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/suite"
)

type BinarySuite struct {
	suite.Suite
}

func TestBinarySuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(BinarySuite))
}

func (s *BinarySuite) TestRead() {
	buf := bytes.NewBuffer(nil)
	err := binary.Write(buf, binary.BigEndian, int64(42))
	s.NoError(err)
	err = binary.Write(buf, binary.BigEndian, int32(42))
	s.NoError(err)

	var i64 int64
	var i32 int32
	err = Read(buf, &i64, &i32)
	s.NoError(err)
	s.Equal(int64(42), i64)
	s.Equal(int32(42), i32)
}

func (s *BinarySuite) TestReadUntil() {
	buf := bytes.NewBuffer([]byte("foo bar"))

	b, err := ReadUntil(buf, ' ')
	s.NoError(err)
	s.Len(b, 3)
	s.Equal("foo", string(b))
}

func (s *BinarySuite) TestReadUntilFromBufioReader() {
	buf := bufio.NewReader(bytes.NewBuffer([]byte("foo bar")))

	b, err := ReadUntilFromBufioReader(buf, ' ')
	s.NoError(err)
	s.Len(b, 3)
	s.Equal("foo", string(b))
}

func (s *BinarySuite) TestReadVariableWidthInt() {
	buf := bytes.NewBuffer([]byte{129, 110})

	i, err := ReadVariableWidthInt(buf)
	s.NoError(err)
	s.Equal(int64(366), i)
}

func (s *BinarySuite) TestReadVariableWidthIntShort() {
	buf := bytes.NewBuffer([]byte{19})

	i, err := ReadVariableWidthInt(buf)
	s.NoError(err)
	s.Equal(int64(19), i)
}

func (s *BinarySuite) TestReadUint32() {
	buf := bytes.NewBuffer(nil)
	err := binary.Write(buf, binary.BigEndian, uint32(42))
	s.NoError(err)

	i32, err := ReadUint32(buf)
	s.NoError(err)
	s.Equal(uint32(42), i32)
}

func (s *BinarySuite) TestReadUint16() {
	buf := bytes.NewBuffer(nil)
	err := binary.Write(buf, binary.BigEndian, uint16(42))
	s.NoError(err)

	i32, err := ReadUint16(buf)
	s.NoError(err)
	s.Equal(uint16(42), i32)
}

func (s *BinarySuite) TestIsBinary() {
	buf := bytes.NewBuffer(nil)
	buf.Write(bytes.Repeat([]byte{'A'}, sniffLen))
	buf.Write([]byte{0})
	ok, err := IsBinary(buf)
	s.NoError(err)
	s.False(ok)

	buf.Reset()

	buf.Write(bytes.Repeat([]byte{'A'}, sniffLen-1))
	buf.Write([]byte{0})
	ok, err = IsBinary(buf)
	s.NoError(err)
	s.True(ok)

	buf.Reset()

	buf.Write(bytes.Repeat([]byte{'A'}, 10))
	ok, err = IsBinary(buf)
	s.NoError(err)
	s.False(ok)
}
