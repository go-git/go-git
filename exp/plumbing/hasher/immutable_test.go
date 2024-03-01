package hasher_test

import (
	"crypto"
	"fmt"
	"hash"
	"strings"
	"testing"

	. "github.com/go-git/go-git/v5/exp/plumbing/hasher"
	"github.com/go-git/go-git/v5/plumbing"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/pjbgf/sha1cd"
	"github.com/stretchr/testify/assert"
)

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
		{"partial sha1", "8ab686eafeb1f44702738", false, true},
		{"partial sha256", "edeaaff3f1774ad28886", false, true},
		{"invalid sha1", "8ab686eafeb1f44702738x", false, true},
		{"invalid sha256", "edeaaff3f1774ad28886x", false, true},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s:%q", tc.name, tc.in), func(t *testing.T) {
			h, ok := FromHex(tc.in)

			assert.Equal(t, tc.ok, ok, "OK did not match")
			if tc.ok {
				assert.Equal(t, tc.empty, h.Empty(), "Empty did not match expectations")
			} else {
				assert.Nil(t, h)
			}
		})
	}
}

func TestZeroFromHash(t *testing.T) {
	tests := []struct {
		name string
		h    hash.Hash
		want string
	}{
		{"valid sha1", crypto.SHA1.New(), strings.Repeat("0", 40)},
		{"valid sha1cd", sha1cd.New(), strings.Repeat("0", 40)},
		{"valid sha256", crypto.SHA256.New(), strings.Repeat("0", 64)},
		{"unsupported hash", crypto.SHA384.New(), strings.Repeat("0", 40)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ZeroFromHash(tc.h)
			assert.Equal(t, tc.want, got.String())
			assert.True(t, got.Empty(), "should be empty")
		})
	}
}

func TestZeroFromObjectFormat(t *testing.T) {
	tests := []struct {
		name string
		of   format.ObjectFormat
		want string
	}{
		{"valid sha1", format.SHA1, strings.Repeat("0", 40)},
		{"valid sha256", format.SHA256, strings.Repeat("0", 64)},
		{"invalid format", "invalid", strings.Repeat("0", 40)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ZeroFromObjectFormat(tc.of)
			assert.Equal(t, tc.want, got.String())
			assert.True(t, got.Empty(), "should be empty")
		})
	}
}

func TestNotLeakingBackingArray(t *testing.T) {
	tests := []struct {
		in  string
		sum []byte
	}{
		{
			"9f361d484fcebb869e1919dc7467b82ac6ca5fad",
			[]byte{
				0x9f, 0x36, 0x1d, 0x48, 0x4f, 0xce, 0xbb, 0x86, 0x9e, 0x19,
				0x19, 0xdc, 0x74, 0x67, 0xb8, 0x2a, 0xc6, 0xca, 0x5f, 0xad,
			},
		},
		{
			"2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe677",
			[]byte{
				0x2c, 0x07, 0xa4, 0x77, 0x3e, 0x3a, 0x95, 0x7c, 0x77, 0x81,
				0x0e, 0x8c, 0xc5, 0xde, 0xb5, 0x2c, 0xd7, 0x04, 0x93, 0x80,
				0x3c, 0x04, 0x8e, 0x48, 0xdc, 0xc0, 0xe0, 0x1f, 0x94, 0xcb,
				0xe6, 0x77,
			},
		},
	}

	for _, tc := range tests {
		h, ok := FromHex(tc.in)
		assert.True(t, ok)
		assert.Equal(t, tc.in, h.String())
		assert.Equal(t, tc.sum, h.Sum())

		sum := h.Sum()
		for i := range sum {
			sum[i] = 0
		}
		assert.Equal(t, tc.sum, h.Sum())
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
			sha1:   "9f361d484fcebb869e1919dc7467b82ac6ca5fxf",
			sha256: "2c07a4773e3a957c77810e8cc5deb52cd70493803c048e48dcc0e01f94cbe6xd",
		},
		{
			name:   "zero",
			sha1:   "0000000000000000000000000000000000000000",
			sha256: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tests {
		b.Run(fmt.Sprintf("hasher-parse-sha1-%s", tc.name), func(b *testing.B) {
			benchmarkHashParse(b, tc.sha1)
		})
		b.Run(fmt.Sprintf("objecthash-fromhex-sha1-%s", tc.name), func(b *testing.B) {
			benchmarkObjectHashParse(b, tc.sha1)
		})
		b.Run(fmt.Sprintf("objecthash-fromhex-sha256-%s", tc.name), func(b *testing.B) {
			benchmarkObjectHashParse(b, tc.sha256)
		})
	}
}

func benchmarkHashParse(b *testing.B, in string) {
	for i := 0; i < b.N; i++ {
		_ = plumbing.NewHash(in)
		b.SetBytes(int64(len(in)))
	}
}

func benchmarkObjectHashParse(b *testing.B, in string) {
	for i := 0; i < b.N; i++ {
		_, _ = FromHex(in)
		b.SetBytes(int64(len(in)))
	}
}
