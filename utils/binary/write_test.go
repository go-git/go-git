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

func (s *BinarySuite) TestWriteNilWriter() {
	err := Write(nil, int64(42))
	s.ErrorContains(err, "nil writer")
}

func (s *BinarySuite) TestWriteUint64() {
	expected := bytes.NewBuffer(nil)
	err := binary.Write(expected, binary.BigEndian, uint64(42))
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = WriteUint64(buf, 42)
	s.NoError(err)
	s.Equal(expected, buf)
}

func (s *BinarySuite) TestWriteUint64NilWriter() {
	err := WriteUint64(nil, 42)
	s.ErrorContains(err, "nil writer")
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

func (s *BinarySuite) TestWriteUint32NilWriter() {
	err := WriteUint32(nil, 42)
	s.ErrorContains(err, "nil writer")
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

func (s *BinarySuite) TestWriteUint16NilWriter() {
	err := WriteUint16(nil, 42)
	s.ErrorContains(err, "nil writer")
}

func (s *BinarySuite) TestWriteVariableWidthInt() {
	buf := bytes.NewBuffer(nil)

	err := WriteVariableWidthInt(buf, 366)
	s.NoError(err)
	s.Equal([]byte{129, 110}, buf.Bytes())
}

func (s *BinarySuite) TestWriteVariableWidthIntNilWriter() {
	err := WriteVariableWidthInt(nil, 366)
	s.ErrorContains(err, "nil writer")
}

func (s *BinarySuite) TestWriteVariableWidthIntShort() {
	buf := bytes.NewBuffer(nil)

	err := WriteVariableWidthInt(buf, 19)
	s.NoError(err)
	s.Equal([]byte{19}, buf.Bytes())
}

func (s *BinarySuite) TestAlign() {
	tests := []struct {
		name      string
		value     uint64
		alignment uint64
		want      uint64
	}{
		{name: "already aligned", value: 8, alignment: 4, want: 8},
		{name: "rounds up", value: 9, alignment: 4, want: 12},
		{name: "zero value", value: 0, alignment: 4, want: 0},
	}

	for _, tt := range tests {
		got, err := Align(tt.value, tt.alignment)
		s.NoError(err, tt.name)
		s.Equal(tt.want, got, tt.name)
	}
}

func (s *BinarySuite) TestAlignInvalidAlignment() {
	_, err := Align(42, 0)
	s.ErrorContains(err, "alignment")
}

func (s *BinarySuite) TestWritePadding() {
	buf := bytes.NewBuffer([]byte{1, 2, 3, 4, 5})

	err := WritePadding(buf, buf.Len(), 4)
	s.NoError(err)
	s.Equal([]byte{1, 2, 3, 4, 5, 0, 0, 0}, buf.Bytes())

	err = WritePadding(buf, buf.Len(), 4)
	s.NoError(err)
	s.Equal([]byte{1, 2, 3, 4, 5, 0, 0, 0}, buf.Bytes())
}

func (s *BinarySuite) TestWritePaddingNilWriter() {
	err := WritePadding(nil, 1, 4)
	s.ErrorContains(err, "nil writer")
}

func (s *BinarySuite) TestWritePaddingInvalidLength() {
	err := WritePadding(bytes.NewBuffer(nil), -1, 4)
	s.ErrorContains(err, "negative")
}

func (s *BinarySuite) TestWritePaddingInvalidAlignment() {
	err := WritePadding(bytes.NewBuffer(nil), 1, 0)
	s.ErrorContains(err, "alignment")
}
