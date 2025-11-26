package plumbing

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/config"
)

var input = strings.Repeat("43aec75c611f22c73b27ece2841e6ccca592f2", 50000000)

func BenchmarkReadFrom(b *testing.B) {
	raw, err := hex.DecodeString(input)
	require.NoError(b, err)

	r := bytes.NewReader(raw)

	b.Run("sha1", func(b *testing.B) {
		r.Reset(raw)
		id := &ObjectID{}
		for i := 0; i < b.N; i++ {
			_, err = id.ReadFrom(r)
			require.NoError(b, err)
			assert.False(b, id.IsZero())
		}
	})
	b.Run("sha256", func(b *testing.B) {
		r.Reset(raw)
		id := &ObjectID{format: config.SHA256}
		for i := 0; i < b.N; i++ {
			_, err = id.ReadFrom(r)
			require.NoError(b, err)
			assert.False(b, id.IsZero())
		}
	})
}

func BenchmarkObjectIDComparison(b *testing.B) {
	id1, _ := hex.DecodeString("43aec75c611f22c73b27ece2841e6ccca592f2")
	id2, _ := hex.DecodeString("43aec75c611f22c73b27ece2841e6ccca592f1")

	b.Run("compare", func(b *testing.B) {
		first, _ := FromBytes(id1)

		for i := 0; i < b.N; i++ {
			first.Compare(id2)
		}
	})

	b.Run("compare + bytes()", func(b *testing.B) {
		first, _ := FromBytes(id1)
		second, _ := FromBytes(id2)

		for i := 0; i < b.N; i++ {
			_ = first.Compare(second.Bytes())
		}
	})

	b.Run("equal", func(b *testing.B) {
		first, _ := FromBytes(id1)
		second, _ := FromBytes(id2)

		for i := 0; i < b.N; i++ {
			_ = first.Equal(second)
		}
	})
}

func TestReadFrom(t *testing.T) {
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
		},
		{
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

			var h ObjectID
			h.ResetBySize(tc.len)
			_, err = h.ReadFrom(buf)

			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.bytes, h.Bytes())
				assert.Equal(t, tc.expected, h.String())
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.True(t, h.IsZero())
			}
		})
	}
}

func TestFromHex(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		ok    bool
		empty bool
	}{
		{"valid sha1", "8ab686eafeb1f44702738c8b0f24f2567c36da6d", true, false},
		{"valid sha256", "edeaaff3f1774ad2888673770c6d64097e391bc362d7d6fb34982ddf0efd18cb", true, false},
		{"empty sha1", "0000000000000000000000000000000000000000", true, true},
		{"empty sha256", "0000000000000000000000000000000000000000000000000000000000000000", true, true},
		{"partial sha1", "8ab686eafeb1f44702738", false, false},
		{"partial sha256", "edeaaff3f1774ad28886", true, false},
		{"invalid sha1", "8ab686eafeb1f44702738x", false, true},
		{"invalid sha256", "edeaaff3f1774ad28886x", false, true},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s:%q", tc.name, tc.in), func(t *testing.T) {
			h, ok := FromHex(tc.in)

			assert.Equal(t, tc.ok, ok, "OK did not match")
			if tc.ok {
				assert.Equal(t, tc.empty, h.IsZero(), "IsZero did not match expectations")
			} else {
				assert.True(t, h.IsZero())
			}
		})
	}
}

func TestFromBytes(t *testing.T) {
	tests := []struct {
		in     []byte
		wantOK bool
		hash   string
	}{
		{
			in: []byte{
				0x9f, 0x36, 0x1d, 0x48, 0x4f, 0xce, 0xbb, 0x86, 0x9e, 0x19,
				0x19, 0xdc, 0x74, 0x67, 0xb8, 0x2a, 0xc6, 0xca, 0x5f, 0xad,
			},
			wantOK: true,
			hash:   "9f361d484fcebb869e1919dc7467b82ac6ca5fad",
		},
		{
			in: []byte{
				0x2c, 0x07, 0xa4, 0x77, 0x3e, 0x3a, 0x95, 0x7c, 0x77, 0x81,
				0x0e, 0x8c, 0xc5, 0xde, 0xb5, 0x2c, 0xd7, 0x04, 0x93, 0x80,
				0x3c, 0x04, 0x8e, 0x48, 0xdc, 0xc0, 0xe0, 0x1f, 0x94, 0xcb,
				0xe6, 0x77,
			},
			wantOK: true,
			hash:   "2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe677",
		},
		{
			in: []byte{
				0x9f, 0x36, 0x1d, 0x48, 0x4f, 0xce, 0xbb, 0x86, 0x9e, 0x19,
			},
			hash: "0000000000000000000000000000000000000000",
		},
		{
			in: []byte{
				0x9f, 0x36, 0x1d, 0x48, 0x4f, 0xce, 0xbb, 0x86, 0x9e, 0x19,
				0x19, 0xdc, 0x74, 0x67, 0xb8, 0x2a, 0xc6, 0xca, 0x5f,
			},
			hash: "0000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tests {
		h, got := FromBytes(tc.in)
		assert.Equal(t, tc.wantOK, got)
		assert.Equal(t, tc.hash, h.String())
	}
}

func BenchmarkHashFromHex(b *testing.B) {
	tests := []struct {
		name   string
		sha1   string
		sha256 string
	}{
		{
			name:   "valid",
			sha1:   "9f361d484fcebb869e1919dc7467b82ac6ca5fad",
			sha256: "2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe677",
		},
		{
			name:   "invalid",
			sha1:   "9f361d484fcebb869e1919dc7467b82ac6ca5fxx",
			sha256: "2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe6xx",
		},
		{
			name:   "zero",
			sha1:   "0000000000000000000000000000000000000000",
			sha256: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tests {
		b.Run(fmt.Sprintf("frombytes-sha1-%s", tc.name), func(b *testing.B) {
			benchmarkHashParse(b, tc.sha1)
		})
		b.Run(fmt.Sprintf("frombytes-sha256-%s", tc.name), func(b *testing.B) {
			benchmarkHashParse(b, tc.sha256)
		})
		b.Run(fmt.Sprintf("fromhex-sha1-%s", tc.name), func(b *testing.B) {
			benchmarkObjectHashParse(b, tc.sha1)
		})
		b.Run(fmt.Sprintf("fromhex-sha256-%s", tc.name), func(b *testing.B) {
			benchmarkObjectHashParse(b, tc.sha256)
		})
	}
}

func benchmarkHashParse(b *testing.B, in string) {
	// decode won't return the invalid hex bytes, so read them.
	data, err := hex.DecodeString(in)
	if err != nil {
		data = append(data, 0x78, 0x78)
	}

	for i := 0; i < b.N; i++ {
		_, _ = FromBytes(data)
		b.SetBytes(int64(len(in)))
	}
}

func benchmarkObjectHashParse(b *testing.B, in string) {
	for i := 0; i < b.N; i++ {
		_, _ = FromHex(in)
		b.SetBytes(int64(len(in)))
	}
}
