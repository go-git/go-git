package idxfile_test

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	. "github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

type IdxfileSuite struct {
	suite.Suite
}

func TestIdxfileSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(IdxfileSuite))
}

func (s *IdxfileSuite) TestDecode() {
	f := fixtures.Basic().One()

	idxf, err := f.Idx()
	s.Require().NoError(err)
	d := NewDecoder(idxf, hash.New(crypto.SHA1))
	idx := new(MemoryIndex)
	err = d.Decode(idx)
	s.NoError(err)

	count, _ := idx.Count()
	s.Equal(int64(31), count)

	hash := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")
	ok, err := idx.Contains(hash)
	s.NoError(err)
	s.True(ok)

	offset, err := idx.FindOffset(hash)
	s.NoError(err)
	s.Equal(int64(615), offset)

	crc32, err := idx.FindCRC32(hash)
	s.NoError(err)
	s.Equal(uint32(3645019190), crc32)

	s.Equal("fb794f1ec720b9bc8e43257451bd99c4be6fa1c9", idx.IdxChecksum.String())
	s.Equal(f.PackfileHash, idx.PackfileChecksum.String())
}

func (s *IdxfileSuite) TestDecode64bitsOffsets() {
	raw, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(fixtureLarge4GB)))
	s.Require().NoError(err)

	idx := new(MemoryIndex)

	d := NewDecoder(FromBytes(raw), hash.New(crypto.SHA1))
	err = d.Decode(idx)
	s.NoError(err)

	expected := map[string]uint64{
		"303953e5aa461c203a324821bc1717f9b4fff895": 12,
		"5296768e3d9f661387ccbff18c4dea6c997fd78c": 142,
		"03fc8d58d44267274edef4585eaeeb445879d33f": 1601322837,
		"8f3ceb4ea4cb9e4a0f751795eb41c9a4f07be772": 2646996529,
		"e0d1d625010087f79c9e01ad9d8f95e1628dda02": 3452385606,
		"90eba326cdc4d1d61c5ad25224ccbf08731dd041": 3707047470,
		"bab53055add7bc35882758a922c54a874d6b1272": 5323223332,
		"1b8995f51987d8a449ca5ea4356595102dc2fbd4": 5894072943,
		"35858be9c6f5914cbe6768489c41eb6809a2bceb": 5924278919,
	}

	iter, err := idx.Entries()
	s.NoError(err)

	var entries int
	for {
		e, err := iter.Next()
		if err == io.EOF {
			break
		}
		s.NoError(err)
		entries++

		s.Equal(e.Offset, expected[e.Hash.String()])
	}

	s.Len(expected, entries)
}

const fixtureLarge4GB = `/3RPYwAAAAIAAAAAAAAAAAAAAAAAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEA
AAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAA
AAEAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAA
AgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAADAAAAAwAAAAMAAAADAAAAAwAAAAQAAAAE
AAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA
AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABQAA
AAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAA
BQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAF
AAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUA
AAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAAAAUAAAAFAAAABQAA
AAUAAAAFAAAABQAAAAYAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAA
BwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAH
AAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcAAAAHAAAABwAAAAcA
AAAHAAAABwAAAAcAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAA
AAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAA
CAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAgAAAAIAAAACAAAAAkAAAAJ
AAAACQAAAAkAAAAJAAAACQAAAAkAAAAJAAAACQAAAAkAAAAJAAAACQAAAAkAAAAJAAAACQAAAAkA
AAAJAAAACQAAAAkAAAAJAAAACQAAAAkAAAAJAAAACQAAAAkAAAAJAAAACQAAAAkAAAAJAAAACQAA
AAkAAAAJA/yNWNRCZydO3vRYXq7rRFh50z8biZX1GYfYpEnKXqQ1ZZUQLcL71DA5U+WqRhwgOjJI
IbwXF/m0//iVNYWL6cb1kUy+Z2hInEHraAmivOtSlnaOPZ9mE4fMv/GMTepsmX/XjI88606ky55K
D3UXletByaTwe+dykOujJs3E0dYcWtJSJMy/CHMd0EG6tTBVrde8NYgnWKkixUqHTWsScuDR1iUB
AIf3nJ4BrZ2PleFijdoCkp36qiGHwFa8NHxMnInZ0s3CKEKmHe+KcZPzuqwmm44GvqGAX3I/VYAA
AAAAAAAMgAAAAQAAAI6AAAACgAAAA4AAAASAAAAFAAAAAV9Qam8AAAABYR1ShwAAAACdxfYxAAAA
ANz1Di4AAAABPUnxJAAAAADNxzlGr6vCJpIFz4XaG/fi/f9C9zgQ8ptKSQpfQ1NMJBGTDTxxYGGp
ch2xUA==
`

func BenchmarkDecode(b *testing.B) {
	f := fixtures.Basic().One()
	idxFile, idxErr := f.Idx()
	if idxErr != nil {
		b.Errorf("unexpected error getting idx: %s", idxErr)
	}
	fixture, err := io.ReadAll(idxFile)
	if err != nil {
		b.Errorf("unexpected error reading idx file: %s", err)
	}

	hasher := hash.New(crypto.SHA1)
	for b.Loop() {
		idx := new(MemoryIndex)
		d := NewDecoder(FromBytes(fixture), hasher)
		if err := d.Decode(idx); err != nil {
			b.Errorf("unexpected error decoding: %s", err)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()

	idx, err := fixtures.Basic().One().Idx()
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })
	validIdx, err := io.ReadAll(idx)
	require.NoError(t, err)

	tests := []struct {
		name            string
		input           func() []byte
		wantErr         error
		wantErrContains string
	}{
		{
			name:    "empty input",
			input:   func() []byte { return nil },
			wantErr: io.EOF,
		},
		{
			name:    "wrong magic",
			input:   func() []byte { return []byte{0, 0, 0, 0, 0, 0, 0, 2} },
			wantErr: ErrMalformedIdxFile,
		},
		{
			name:    "truncated header",
			input:   func() []byte { return []byte{255, 't'} },
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name: "unsupported version 1",
			input: func() []byte {
				var buf bytes.Buffer
				buf.Write([]byte{255, 't', 'O', 'c'})
				binary.Write(&buf, binary.BigEndian, uint32(1))
				return buf.Bytes()
			},
			wantErr:         ErrUnsupportedVersion,
			wantErrContains: "v1",
		},
		{
			name: "unsupported version 3",
			input: func() []byte {
				var buf bytes.Buffer
				buf.Write([]byte{255, 't', 'O', 'c'})
				binary.Write(&buf, binary.BigEndian, uint32(3))
				return buf.Bytes()
			},
			wantErr:         ErrUnsupportedVersion,
			wantErrContains: "v3",
		},
		{
			name: "truncated fanout table",
			input: func() []byte {
				buf := idxV2Header()
				// Only 10 fanout entries instead of 256.
				for range 10 {
					buf = binary.BigEndian.AppendUint32(buf, 0)
				}
				return buf
			},
			wantErr: io.EOF,
		},
		{
			name: "non-monotonic fanout at entry 1",
			input: func() []byte {
				buf := idxV2Header()
				// entry[0]=5, entry[1]=3 (decrease), rest=5
				buf = append(buf, writeFanout(5, map[int]uint32{0: 5, 1: 3})...)
				return buf
			},
			wantErr:         ErrMalformedIdxFile,
			wantErrContains: "not monotonically non-decreasing",
		},
		{
			name: "non-monotonic fanout at last entry",
			input: func() []byte {
				buf := idxV2Header()
				// all entries = 10, except entry[255] = 5
				buf = append(buf, writeFanout(10, map[int]uint32{255: 5})...)
				return buf
			},
			wantErr:         ErrMalformedIdxFile,
			wantErrContains: "not monotonically non-decreasing",
		},
		{
			name: "truncated object names",
			input: func() []byte {
				buf := idxV2Header()
				// Fanout claims 1 object, but no name data follows.
				buf = append(buf, writeFanout(1, nil)...)
				return buf
			},
			// The size formula now rejects the input before the
			// reader gets a chance to report EOF: a single-object
			// idx v2 needs at least 1100 bytes for SHA-1, and the
			// header+fanout alone is only 1032.
			wantErr:         ErrMalformedIdxFile,
			wantErrContains: "inconsistent with object count",
		},
		{
			name: "checksum mismatch",
			input: func() []byte {
				corrupted := make([]byte, len(validIdx))
				copy(corrupted, validIdx)
				// Flip the last byte of the idx checksum.
				corrupted[len(corrupted)-1] ^= 0xff
				return corrupted
			},
			wantErr:         ErrMalformedIdxFile,
			wantErrContains: "checksum mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			idx := new(MemoryIndex)
			d := NewDecoder(FromBytes(tt.input()), hash.New(crypto.SHA1))

			err := d.Decode(idx)
			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantErrContains != "" {
				require.ErrorContains(t, err, tt.wantErrContains)
			}
		})
	}
}

// writeFanout writes a 256-entry fanout table where every entry is set to total,
// except for overrides specified as index→value pairs applied afterwards.
func writeFanout(total uint32, overrides map[int]uint32) []byte {
	var buf bytes.Buffer
	entries := [256]uint32{}
	for i := range entries {
		entries[i] = total
	}
	for k, v := range overrides {
		entries[k] = v
	}
	for _, v := range entries {
		binary.Write(&buf, binary.BigEndian, v)
	}
	return buf.Bytes()
}

// idxV2Header returns the 8-byte idx v2 header (magic + version).
func idxV2Header() []byte {
	var buf bytes.Buffer
	buf.Write([]byte{255, 't', 'O', 'c'})
	binary.Write(&buf, binary.BigEndian, uint32(2))
	return buf.Bytes()
}

// TestDecoderSizeFormulaBoundary exercises the [minSize, maxSize] range
// enforced by validateIdxV2Size. The boundary is asymmetric for nr > 1:
// each extra 8-byte offset64 slot extends maxSize by 8. Inputs at the
// edge of the legal range must pass the size check (failing later with
// the trailing checksum mismatch, since the payload is zero-filled);
// inputs one byte outside must be rejected with a size-related error.
func TestDecoderSizeFormulaBoundary(t *testing.T) {
	t.Parallel()

	const hashsz = 20 // SHA-1
	const headerLen = 8 + 4*256
	minSize := func(nr int64) int64 {
		return headerLen + nr*(hashsz+8) + 2*hashsz
	}
	maxSize := func(nr int64) int64 {
		m := minSize(nr)
		if nr > 0 {
			m += (nr - 1) * 8
		}
		return m
	}
	build := func(nr uint32, total int64) []byte {
		buf := append(idxV2Header(), writeFanout(nr, nil)...)
		if int64(len(buf)) > total {
			t.Fatalf("header+fanout exceeds requested total: %d > %d", len(buf), total)
		}
		return append(buf, make([]byte, total-int64(len(buf)))...)
	}

	tests := []struct {
		name      string
		nr        uint32
		size      int64
		passesLen bool // true: size check passes (later parsing may fail)
	}{
		{"nr=1, at minSize", 1, minSize(1), true},
		{"nr=1, one byte below minSize", 1, minSize(1) - 1, false},
		{"nr=1, one byte above maxSize", 1, maxSize(1) + 1, false},
		{"nr=2, at minSize", 2, minSize(2), true},
		{"nr=2, at maxSize", 2, maxSize(2), true},
		{"nr=2, one byte below minSize", 2, minSize(2) - 1, false},
		{"nr=2, one byte above maxSize", 2, maxSize(2) + 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			idx := new(MemoryIndex)
			d := NewDecoder(FromBytes(build(tt.nr, tt.size)), hash.New(crypto.SHA1))
			err := d.Decode(idx)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrMalformedIdxFile)
			if tt.passesLen {
				// Payload is zero-filled, so we reach the trailing
				// checksum comparison and fail there. The size check
				// itself must not fire.
				require.ErrorContains(t, err, "checksum mismatch")
			} else {
				require.ErrorContains(t, err, "inconsistent with object count")
			}
		})
	}
}

func TestDecoderRejectsInconsistentObjectCount(t *testing.T) {
	t.Parallel()

	// Header (\xff t O c) + version 2 + fanout where fanout[0..255] all
	// claim 0x4C4C4C4C objects. The byte count cannot possibly accommodate
	// that many object names.
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 't', 'O', 'c', 0, 0, 0, 2})
	for range 256 {
		_ = binary.Write(&buf, binary.BigEndian, uint32(0x4C4C4C4C))
	}

	idx := new(MemoryIndex)
	d := NewDecoder(FromBytes(buf.Bytes()), hash.New(crypto.SHA1))
	err := d.Decode(idx)
	if !errors.Is(err, ErrMalformedIdxFile) {
		t.Fatalf("expected ErrMalformedIdxFile, got %v", err)
	}
}
