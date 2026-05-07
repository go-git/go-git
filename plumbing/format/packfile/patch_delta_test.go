package packfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestDecodeLEB128Overflow(t *testing.T) {
	t.Parallel()

	// Eleven continuation bytes is enough to push shift past the bit width
	// of uint on either 32- or 64-bit platforms.
	input := append(bytes.Repeat([]byte{0x80}, 11), 0x01)

	_, _, err := decodeLEB128(input)
	require.ErrorIs(t, err, ErrLengthOverflow)
}

func TestDecodeLEB128ByteReaderOverflow(t *testing.T) {
	t.Parallel()

	input := bytes.Repeat([]byte{0x80}, 11)

	_, err := decodeLEB128ByteReader(bytes.NewReader(input))
	require.ErrorIs(t, err, ErrLengthOverflow)
}

func TestDecodeLEB128(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		want     uint
		wantRest []byte
	}{
		{
			name:     "single byte, small number",
			input:    []byte{0x01, 0xFF},
			want:     1,
			wantRest: []byte{0xFF},
		},
		{
			name:     "single byte, max value without continuation",
			input:    []byte{0x7F, 0xFF},
			want:     127,
			wantRest: []byte{0xFF},
		},
		{
			name:     "two bytes",
			input:    []byte{0x80, 0x01, 0xFF},
			want:     128,
			wantRest: []byte{0xFF},
		},
		{
			name:     "two bytes, larger number",
			input:    []byte{0xFF, 0x01, 0xFF},
			want:     255,
			wantRest: []byte{0xFF},
		},
		{
			name:     "three bytes",
			input:    []byte{0x80, 0x80, 0x01, 0xFF},
			want:     16384,
			wantRest: []byte{0xFF},
		},
		{
			name:     "empty remaining bytes",
			input:    []byte{0x01},
			want:     1,
			wantRest: []byte{},
		},
		{
			name:     "empty input",
			input:    []byte{},
			want:     0,
			wantRest: []byte{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotNum, gotRest, err := decodeLEB128(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, gotNum, "decoded number mismatch")
			assert.Equal(t, tc.wantRest, gotRest, "remaining bytes mismatch")
		})
	}
}

// buildDelta assembles a delta byte stream from a (srcSz, targetSz)
// header and a sequence of pre-encoded operations.
func buildDelta(srcSz, targetSz int, ops ...[]byte) []byte {
	var b bytes.Buffer
	b.Write(deltaEncodeSize(srcSz))
	b.Write(deltaEncodeSize(targetSz))
	for _, op := range ops {
		b.Write(op)
	}
	return b.Bytes()
}

// insertOp encodes a copy-from-delta op of the given payload.
func insertOp(data []byte) []byte {
	return append([]byte{byte(len(data))}, data...)
}

// TestPatchDeltaRejectsOversizedCopies asserts that a delta whose
// individual copy operations each fit within the declared target size,
// but whose cumulative output would exceed it, is rejected before any
// write past the declared target size happens.
func TestPatchDeltaRejectsOversizedCopies(t *testing.T) {
	t.Parallel()

	src := bytes.Repeat([]byte("A"), 64)

	cases := []struct {
		name     string
		targetSz uint
		delta    []byte
	}{
		{
			// Two copy-from-src ops, each individually fits but
			// their sum (126) exceeds targetSz (64).
			name:     "copy-from-src cumulative overflow",
			targetSz: 64,
			delta: buildDelta(64, 64,
				encodeCopyOperation(0, 63),
				encodeCopyOperation(0, 63),
			),
		},
		{
			// Two copy-from-delta ops, each fits but their sum (14)
			// exceeds targetSz (10).
			name:     "copy-from-delta cumulative overflow",
			targetSz: 10,
			delta: buildDelta(64, 10,
				insertOp(bytes.Repeat([]byte{'x'}, 7)),
				insertOp(bytes.Repeat([]byte{'x'}, 7)),
			),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := PatchDelta(src, tc.delta)
			assert.ErrorIs(t, err, ErrInvalidDelta)

			// The buffer fed to patchDelta must never be written past
			// targetSz: that is the property the validation protects.
			b := &bytes.Buffer{}
			_ = patchDelta(b, src, tc.delta)
			assert.LessOrEqual(t, uint(b.Len()), tc.targetSz,
				"patchDelta wrote past the declared target size")
		})
	}
}

// TestReaderFromDeltaRejectsOversizedCopies covers the streaming
// counterpart and asserts that the reader surfaces ErrInvalidDelta to
// the consumer rather than silently truncating the stream when a
// crafted delta would write past the declared target size.
func TestReaderFromDeltaRejectsOversizedCopies(t *testing.T) {
	t.Parallel()

	src := bytes.Repeat([]byte("A"), 64)
	base := &plumbing.MemoryObject{}
	_, _ = base.Write(src)

	delta := buildDelta(64, 64,
		encodeCopyOperation(0, 63),
		encodeCopyOperation(0, 63),
	)

	rc, err := ReaderFromDelta(base, io.NopCloser(bytes.NewReader(delta)))
	assert.NoError(t, err)
	out, err := io.ReadAll(rc)
	assert.ErrorIs(t, err, ErrInvalidDelta)
	assert.LessOrEqual(t, len(out), 64,
		"ReaderFromDelta yielded more bytes than the declared target size")
}

// TestPatchDeltaRejectsTrailingBytes asserts that a delta whose
// operations exactly fill the declared target size but is followed by
// extra bytes is rejected, matching upstream's `data != top` post-loop
// sanity check.
func TestPatchDeltaRejectsTrailingBytes(t *testing.T) {
	t.Parallel()

	src := bytes.Repeat([]byte("A"), 64)
	delta := buildDelta(64, 64,
		encodeCopyOperation(0, 64),
		[]byte{0x00, 0x01, 0x02}, // unused trailing bytes
	)

	_, err := PatchDelta(src, delta)
	assert.ErrorIs(t, err, ErrInvalidDelta)
}

// TestPatchDeltaAcceptsEmptyTarget asserts that a delta whose declared
// target size is zero and which carries no operations succeeds and
// produces an empty result, matching upstream's behaviour of treating
// `data == top && size == 0` as success.
func TestPatchDeltaAcceptsEmptyTarget(t *testing.T) {
	t.Parallel()

	src := []byte("hello")
	delta := buildDelta(len(src), 0)

	out, err := PatchDelta(src, delta)
	assert.NoError(t, err)
	assert.Empty(t, out)
}
