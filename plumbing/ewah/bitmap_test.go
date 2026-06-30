package ewah_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/ewah"
)

// makeRLW assembles a run-length word from its components.
func makeRLW(runBit bool, runLen, literalWords uint64) uint64 {
	var w uint64
	if runBit {
		w |= 1
	}
	w |= (runLen & ewah.RLWLargestRunningCount) << 1
	w |= literalWords << (1 + ewah.RLWRunningBits)
	return w
}

// buildEWAH serialises an EWAH bitmap with the given logical bit size and
// compressed words, in the on-disk layout that ReadFrom expects.
func buildEWAH(t *testing.T, bitSize uint32, words []uint64) []byte {
	t.Helper()

	var buf bytes.Buffer
	for _, v := range []any{bitSize, uint32(len(words)), words, uint32(0)} {
		require.NoError(t, binary.Write(&buf, binary.BigEndian, v))
	}
	return buf.Bytes()
}

// collect returns the set bit positions reported by ForEach.
func collect(b *ewah.Bitmap) []uint64 {
	var got []uint64
	b.ForEach(func(pos uint64) bool {
		got = append(got, pos)
		return true
	})
	return got
}

func TestRLWAccessors(t *testing.T) {
	t.Parallel()

	rlw := makeRLW(true, 12345, 678)
	assert.True(t, ewah.RunBit(rlw))
	assert.Equal(t, uint64(12345), ewah.RunningLen(rlw))
	assert.Equal(t, uint64(678), ewah.LiteralWords(rlw))

	rlw = makeRLW(false, 0, 0)
	assert.False(t, ewah.RunBit(rlw))
	assert.Equal(t, uint64(0), ewah.RunningLen(rlw))
	assert.Equal(t, uint64(0), ewah.LiteralWords(rlw))
}

func TestBitmap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bitSize uint32
		words   []uint64
		set     []uint64 // expected set positions, in order
		numBits uint64
	}{
		{
			name:    "single literal word",
			bitSize: 5,
			words:   []uint64{makeRLW(false, 0, 1), 0b10101},
			set:     []uint64{0, 2, 4},
			numBits: 64,
		},
		{
			name:    "run of set words",
			bitSize: 128,
			words:   []uint64{makeRLW(true, 2, 0)},
			set:     seq(0, 128),
			numBits: 128,
		},
		{
			name:    "run then literal",
			bitSize: 65,
			words:   []uint64{makeRLW(true, 1, 1), 0b1},
			set:     append(seq(0, 64), 64),
			numBits: 128,
		},
		{
			name:    "multiple literal words",
			bitSize: 128,
			words:   []uint64{makeRLW(false, 0, 2), 0, 1 << 5},
			set:     []uint64{64 + 5},
			numBits: 128,
		},
		{
			name:    "empty",
			bitSize: 0,
			words:   nil,
			set:     nil,
			numBits: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw := buildEWAH(t, tc.bitSize, tc.words)

			b, err := ewah.ReadFrom(bytes.NewReader(raw))
			require.NoError(t, err)

			assert.Equal(t, uint64(tc.bitSize), b.Bits())
			assert.Equal(t, tc.numBits, b.NumBits())

			// ForEach must report exactly the expected positions.
			assert.Equal(t, tc.set, collect(b))

			// At must agree with the set returned by ForEach across the whole
			// encoded range, and report false beyond it.
			want := make(map[uint64]bool, len(tc.set))
			for _, p := range tc.set {
				want[p] = true
			}
			for i := uint64(0); i < b.NumBits()+64; i++ {
				assert.Equalf(t, want[i], b.At(i), "At(%d)", i)
			}

			// WriteTo must reproduce the input bytes verbatim.
			var out bytes.Buffer
			n, err := b.WriteTo(&out)
			require.NoError(t, err)
			assert.Equal(t, int64(len(raw)), n)
			assert.Equal(t, raw, out.Bytes())
		})
	}
}

func TestForEachStopsEarly(t *testing.T) {
	t.Parallel()

	raw := buildEWAH(t, 5, []uint64{makeRLW(false, 0, 1), 0b10101})
	b, err := ewah.ReadFrom(bytes.NewReader(raw))
	require.NoError(t, err)

	var got []uint64
	b.ForEach(func(pos uint64) bool {
		got = append(got, pos)
		return pos < 2 // stop after emitting the second set bit (position 2)
	})
	assert.Equal(t, []uint64{0, 2}, got)
}

func TestReadFromNilReader(t *testing.T) {
	t.Parallel()

	b, err := ewah.ReadFrom(nil)
	assert.Nil(t, b)
	assert.ErrorIs(t, err, ewah.ErrNilReader)
}

func TestReadFromTruncated(t *testing.T) {
	t.Parallel()

	// Header announces two words but the payload is empty.
	raw := []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x02}
	_, err := ewah.ReadFrom(bytes.NewReader(raw))
	assert.Error(t, err)
}

// seq returns the integers in [start, end).
func seq(start, end uint64) []uint64 {
	out := make([]uint64, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, i)
	}
	return out
}
