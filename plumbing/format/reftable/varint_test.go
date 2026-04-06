package reftable

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVarintRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []uint64{
		0, 1, 2, 127, 128, 129, 255, 256,
		16383, 16384, 16385,
		1<<21 - 1, 1 << 21, 1 << 28,
		1<<35 - 1, 1 << 35,
		1<<63 - 1,
	}

	for _, val := range tests {
		var buf [10]byte
		n := putVarint(buf[:], val)
		require.Greater(t, n, 0, "putVarint should write at least 1 byte for %d", val)

		got, m := getVarint(buf[:n])
		assert.Equal(t, val, got, "round-trip failed for %d", val)
		assert.Equal(t, n, m, "consumed bytes mismatch for %d", val)
	}
}

func TestVarintKnownEncodings(t *testing.T) {
	t.Parallel()
	// Value 0 should encode as a single byte 0x00.
	var buf [10]byte
	n := putVarint(buf[:], 0)
	assert.Equal(t, 1, n)
	assert.Equal(t, byte(0x00), buf[0])

	// Value 127 should encode as a single byte 0x7f.
	n = putVarint(buf[:], 127)
	assert.Equal(t, 1, n)
	assert.Equal(t, byte(0x7f), buf[0])

	// Value 128 should encode as two bytes: 0x80 0x00.
	n = putVarint(buf[:], 128)
	assert.Equal(t, 2, n)
	assert.Equal(t, byte(0x80), buf[0])
	assert.Equal(t, byte(0x00), buf[1])
}

func TestReadVarint(t *testing.T) {
	t.Parallel()
	tests := []uint64{0, 1, 127, 128, 256, 16384, 1 << 28}
	for _, val := range tests {
		var buf [10]byte
		n := putVarint(buf[:], val)

		got, err := readVarint(bytes.NewReader(buf[:n]))
		require.NoError(t, err)
		assert.Equal(t, val, got)
	}
}

func TestGetVarintTruncated(t *testing.T) {
	t.Parallel()
	// A continuation byte with no follow-up should return 0, 0.
	_, n := getVarint([]byte{0x80})
	assert.Equal(t, 0, n)
}

func TestGetVarintEmpty(t *testing.T) {
	t.Parallel()
	_, n := getVarint(nil)
	assert.Equal(t, 0, n)
}
