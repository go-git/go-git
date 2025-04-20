package binary

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type BinarySuite struct {
	suite.Suite
}

func TestBinarySuite(t *testing.T) {
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

func TestReadHash(t *testing.T) {
	tests := []struct {
		expected string
		bytes    []byte
		len      int
		wantErr  string
	}{
		{
			expected: "43aec75c611f22c73b27ece2841e6ccca592f285",
			bytes:    []byte{67, 174, 199, 92, 97, 31, 34, 199, 59, 39, 236, 226, 132, 30, 108, 204, 165, 146, 242, 133},
			len:      20,
		}, {
			expected: "3b27ece2841e6ccca592f28543aec75c611f22c73b27ece2841e6ccca592f285",
			bytes:    []byte{59, 39, 236, 226, 132, 30, 108, 204, 165, 146, 242, 133, 67, 174, 199, 92, 97, 31, 34, 199, 59, 39, 236, 226, 132, 30, 108, 204, 165, 146, 242, 133},
			len:      32,
		},
		{
			expected: "43aec75c611f22c73b27ece2841e6ccca592f2",
			bytes:    []byte{67, 174, 199, 92, 97, 31, 34, 199, 59, 39, 236, 226, 132, 30, 108, 204, 165, 146, 242},
			len:      20,
			wantErr:  "EOF",
		},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := binary.Write(buf, binary.BigEndian, tc.bytes)
			require.NoError(t, err)

			hash, err := ReadHash(buf, tc.len)

			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.bytes, hash.Bytes())
				assert.Equal(t, tc.expected, hash.String())
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.True(t, hash.IsZero())
			}
		})
	}
}

var input = strings.Repeat("43aec75c611f22c73b27ece2841e6ccca592f2", 50000000)

func BenchmarkReadHash(b *testing.B) {
	raw, err := hex.DecodeString(input)
	require.NoError(b, err)

	r := bytes.NewReader(raw)
	for i := 0; i < b.N; i++ {
		h, err := ReadHash(r, 20)
		require.NoError(b, err)
		assert.False(b, h.IsZero())
	}
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
