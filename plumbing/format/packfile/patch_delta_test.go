package packfile

import (
	"bytes"
	"io"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
)

func TestDecodeLEB128Overflow(t *testing.T) {
	t.Parallel()

	input := append(bytes.Repeat([]byte{0x80}, 11), 0x01)

	_, _, err := packutil.DecodeLEB128(input)
	require.ErrorIs(t, err, packutil.ErrLengthOverflow)
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotNum, gotRest, err := packutil.DecodeLEB128(tc.input)
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
	b.Write(packutil.EncodeLEB128(uint(srcSz)))
	b.Write(packutil.EncodeLEB128(uint(targetSz)))
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

	_, err := ReaderFromDelta(base, io.NopCloser(bytes.NewReader(delta)))
	assert.ErrorIs(t, err, ErrInvalidDelta)
}

// TestReaderFromDeltaRejectsShortCopyFromDelta asserts that a
// copy-from-delta operation declaring more bytes than the payload holds
// is rejected. The stream used to end short of the declared target and
// report success, handing the consumer a truncated object with no error.
func TestReaderFromDeltaRejectsShortCopyFromDelta(t *testing.T) {
	t.Parallel()

	base := &plumbing.MemoryObject{}

	// targetSz is 5, and the single operation declares 5 literal bytes
	// while supplying 2.
	delta := buildDelta(0, 5, []byte{0x05, 'a', 'b'})

	rc, err := ReaderFromDelta(base, io.NopCloser(bytes.NewReader(delta)))
	if err == nil {
		var out []byte
		out, err = io.ReadAll(rc)
		assert.Empty(t, out, "yielded a truncated object")
	}
	assert.ErrorIs(t, err, ErrInvalidDelta)
}

// TestReaderFromDeltaRejectsOversizedTarget asserts that the streaming
// path rejects a target its operations cannot reach, like patchDelta,
// rather than producing every byte the operations encode before
// discovering the shortfall. The operations below expand to
// opCount*maxCopySize bytes, which a consumer buffering the stream
// would have to hold.
func TestReaderFromDeltaRejectsOversizedTarget(t *testing.T) {
	t.Parallel()

	const (
		srcSz   = maxCopySize
		opCount = 1024
	)

	base := &plumbing.MemoryObject{}
	_, _ = base.Write(randBytes(srcSz))

	delta := buildDelta(srcSz, math.MaxInt,
		bytes.Repeat([]byte{maskContinue}, opCount))

	rc, err := ReaderFromDelta(base, io.NopCloser(bytes.NewReader(delta)))
	var n int64
	if err == nil {
		n, err = io.Copy(io.Discard, rc)
	}
	assert.ErrorIs(t, err, ErrInvalidDelta)
	assert.Zero(t, n, "streamed output for a target the operations cannot reach")
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

func TestValidateDeltaOps(t *testing.T) {
	t.Parallel()

	const srcSz = maxCopySize

	tests := []struct {
		name     string
		delta    []byte
		targetSz uint
		err      error
	}{
		{
			name:     "no operations for an empty target",
			delta:    nil,
			targetSz: 0,
		},
		{
			name:     "copy from delta",
			delta:    insertOp([]byte("abc")),
			targetSz: 3,
		},
		{
			name:     "copy from source",
			delta:    encodeCopyOperation(0, 64),
			targetSz: 64,
		},
		{
			// A copy-from-src command with no size bits set means
			// maxCopySize, which the source is exactly large enough for.
			name:     "copy from source with implied size",
			delta:    []byte{maskContinue},
			targetSz: maxCopySize,
		},
		{
			name:     "no operations for a non-empty target",
			delta:    nil,
			targetSz: 1,
			err:      ErrInvalidDelta,
		},
		{
			name:     "operations short of the target",
			delta:    encodeCopyOperation(0, 64),
			targetSz: 128,
			err:      ErrInvalidDelta,
		},
		{
			name:     "operations past the target",
			delta:    append(encodeCopyOperation(0, 64), insertOp([]byte("a"))...),
			targetSz: 64,
			err:      ErrInvalidDelta,
		},
		{
			name:     "copy from delta truncated",
			delta:    []byte{0x03, 'a'},
			targetSz: 3,
			err:      ErrInvalidDelta,
		},
		{
			name:     "copy from source past the end",
			delta:    encodeCopyOperation(srcSz, 1),
			targetSz: 1,
			err:      ErrInvalidDelta,
		},
		{
			name:     "reserved command",
			delta:    []byte{0x00},
			targetSz: 1,
			err:      ErrDeltaCmd,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateDeltaOps(tc.delta, srcSz, tc.targetSz)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestDeltaRejectsTruncatedHeader asserts that a header size whose last
// byte still carries the LEB128 continuation bit is rejected, rather
// than decoded as the partial value it happens to have accumulated.
//
// The payload is long enough to satisfy minDeltaSize, so nothing but a
// termination check stands between it and a delta that reports success.
func TestDeltaRejectsTruncatedHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		delta []byte
	}{
		{
			name:  "truncated srcSz",
			delta: []byte{maskContinue, maskContinue},
		},
		{
			// srcSz terminates, then targetSz runs off the end.
			name:  "truncated targetSz",
			delta: []byte{0x00, maskContinue},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var dst bytes.Buffer
			err := patchDelta(&dst, nil, tc.delta)
			assert.ErrorIs(t, err, ErrInvalidDelta, "patchDelta")

			var out bytes.Buffer
			_, _, err = patchDeltaWriter(&out, bytes.NewReader(nil), 0, tc.delta,
				plumbing.BlobObject, nil, format.SHA1)
			assert.ErrorIs(t, err, ErrInvalidDelta, "patchDeltaWriter")
		})
	}
}

// TestPatchDeltaWriterRejectsShortDelta asserts that a payload too
// short to hold both header fields is rejected, matching PatchDelta.
func TestPatchDeltaWriterRejectsShortDelta(t *testing.T) {
	t.Parallel()

	for _, delta := range [][]byte{nil, {0x00}} {
		var dst bytes.Buffer
		_, _, err := patchDeltaWriter(&dst, bytes.NewReader(nil), 0, delta,
			plumbing.BlobObject, nil, format.SHA1)
		assert.ErrorIs(t, err, ErrInvalidDelta)
	}
}

// allocatedBytes reports the number of bytes f allocates. The counter it
// reads is process-wide, so callers must not run it in parallel with
// other tests.
func allocatedBytes(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestOversizedTargetDeltaIsRejectedUpFront covers oss-fuzz issue
// 5764827075903488: a delta that advertises a target size its operations
// cannot produce must be rejected before any of the target is built, not
// after the expansion has already been paid for and thrown away.
//
// Each operation here is a bare copy-from-src command: a single delta
// byte that copies maxCopySize bytes out of the source. The operations
// therefore expand to opCount*maxCopySize bytes before the payload runs
// out short of the advertised target.
func TestOversizedTargetDeltaIsRejectedUpFront(t *testing.T) { //nolint:paralleltest // reads a process-wide allocation counter
	const (
		srcSz   = maxCopySize
		opCount = 1024

		// What the operations expand to before the shortfall surfaces.
		// The bound is two orders of magnitude below that, which leaves
		// room for incidental allocations while still failing loudly if
		// the expansion is reintroduced.
		expansion = opCount * maxCopySize
		bound     = expansion / 128
	)

	src := randBytes(srcSz)
	delta := buildDelta(srcSz, math.MaxInt,
		bytes.Repeat([]byte{maskContinue}, opCount))

	t.Run("PatchDelta", func(t *testing.T) { //nolint:paralleltest // reads a process-wide allocation counter
		var err error
		allocated := allocatedBytes(func() {
			_, err = PatchDelta(src, delta)
		})

		assert.ErrorIs(t, err, ErrInvalidDelta)
		assert.Less(t, allocated, uint64(bound),
			"delta expanded into memory before being rejected")
	})

	t.Run("patchDeltaWriter", func(t *testing.T) { //nolint:paralleltest // reads a process-wide allocation counter
		var err error
		allocated := allocatedBytes(func() {
			var dst bytes.Buffer
			_, _, err = patchDeltaWriter(&dst, bytes.NewReader(src), int64(len(src)), delta,
				plumbing.BlobObject, nil, format.SHA1)
		})

		assert.ErrorIs(t, err, ErrInvalidDelta)
		assert.Less(t, allocated, uint64(bound),
			"delta expanded into memory before being rejected")
	})
}
