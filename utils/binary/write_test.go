package binary

import (
	"bytes"
	"encoding/binary"
)

func (s *BinarySuite) TestWrite() {
	expected := bytes.NewBuffer(nil)
	err := binary.Write(expected, binary.BigEndian, int64(42))
	s.NoError(err)
	err = binary.Write(expected, binary.BigEndian, int32(42))
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = Write(buf, int64(42), int32(42))
	s.NoError(err)
	s.Equal(expected, buf)
}

func (s *BinarySuite) TestWriteUint32() {
	expected := bytes.NewBuffer(nil)
	err := binary.Write(expected, binary.BigEndian, int32(42))
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = WriteUint32(buf, 42)
	s.NoError(err)
	s.Equal(expected, buf)
}

func (s *BinarySuite) TestWriteUint16() {
	expected := bytes.NewBuffer(nil)
	err := binary.Write(expected, binary.BigEndian, int16(42))
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = WriteUint16(buf, 42)
	s.NoError(err)
	s.Equal(expected, buf)
}

func (s *BinarySuite) TestWriteVariableWidthInt() {
	buf := bytes.NewBuffer(nil)

	err := WriteVariableWidthInt(buf, 366)
	s.NoError(err)
	s.Equal([]byte{129, 110}, buf.Bytes())
}

func (s *BinarySuite) TestWriteVariableWidthIntShort() {
	buf := bytes.NewBuffer(nil)

	err := WriteVariableWidthInt(buf, 19)
	s.NoError(err)
	s.Equal([]byte{19}, buf.Bytes())
}
