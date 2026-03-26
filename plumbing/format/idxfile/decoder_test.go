package idxfile_test

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
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

	d := NewDecoder(f.Idx(), hash.New(crypto.SHA1))
	idx := new(MemoryIndex)
	err := d.Decode(idx)
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
	f := bytes.NewBufferString(fixtureLarge4GB)

	idx := new(MemoryIndex)

	d := NewDecoder(base64.NewDecoder(base64.StdEncoding, f), hash.New(crypto.SHA1))
	err := d.Decode(idx)
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
		if errors.Is(err, io.EOF) {
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
	fixture, err := io.ReadAll(f.Idx())
	if err != nil {
		b.Errorf("unexpected error reading idx file: %s", err)
	}

	hasher := hash.New(crypto.SHA1)
	for b.Loop() {
		f := bytes.NewBuffer(fixture)
		idx := new(MemoryIndex)
		d := NewDecoder(f, hasher)
		if err := d.Decode(idx); err != nil {
			b.Errorf("unexpected error decoding: %s", err)
		}
	}
}

func TestChecksumMismatch(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "temp.idx")
	require.NoError(t, err)
	defer f.Close()

	_, err = io.Copy(f, fixtures.Basic().One().Idx())
	require.NoError(t, err)

	_, err = f.Seek(-1, io.SeekEnd)
	require.NoError(t, err)

	_, err = f.Write([]byte{0})
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	idx := new(MemoryIndex)
	d := NewDecoder(f, hash.New(crypto.SHA1))

	err = d.Decode(idx)
	require.ErrorContains(t, err, "checksum mismatch")
}

// TestDecodeLazy verifies that DecodeLazy decodes a known fixture correctly.
func (s *IdxfileSuite) TestDecodeLazy() {
	f := fixtures.Basic().One()

	// Build an in-memory rev from the fixture's .rev file.
	revData, err := io.ReadAll(f.Rev())
	s.Require().NoError(err)

	openRev := func() (ReadAtCloser, error) {
		return NewBytesReadAtCloser(revData), nil
	}

	packHash := plumbing.NewHash(f.PackfileHash)
	idx, err := DecodeLazy(f.Idx(), hash.New(crypto.SHA1), openRev, packHash)
	s.Require().NoError(err)
	s.Require().NotNil(idx)
	s.T().Cleanup(func() { s.Require().NoError(idx.Close()) })

	count, err := idx.Count()
	s.Require().NoError(err)
	s.Equal(int64(31), count)

	h := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")

	ok, err := idx.Contains(h)
	s.Require().NoError(err)
	s.True(ok)

	offset, err := idx.FindOffset(h)
	s.Require().NoError(err)
	s.Equal(int64(615), offset)

	crc32, err := idx.FindCRC32(h)
	s.Require().NoError(err)
	s.Equal(uint32(3645019190), crc32)
}

// TestDecodeLazyEquivalence verifies that DecodeLazy and Decoder.Decode
// produce identical results for every Index method.
func (s *IdxfileSuite) TestDecodeLazyEquivalence() {
	f := fixtures.Basic().One()

	// Decode via MemoryIndex (reference).
	idxBytes, err := io.ReadAll(f.Idx())
	s.Require().NoError(err)

	memIdx := new(MemoryIndex)
	d := NewDecoder(bytes.NewReader(idxBytes), hash.New(crypto.SHA1))
	s.Require().NoError(d.Decode(memIdx))

	// Build a rev for DecodeLazy.
	revData, err := io.ReadAll(f.Rev())
	s.Require().NoError(err)
	openRev := func() (ReadAtCloser, error) {
		return NewBytesReadAtCloser(revData), nil
	}

	packHash := plumbing.NewHash(f.PackfileHash)
	lazyIdx, err := DecodeLazy(bytes.NewReader(idxBytes), hash.New(crypto.SHA1), openRev, packHash)
	s.Require().NoError(err)
	s.T().Cleanup(func() { s.Require().NoError(lazyIdx.Close()) })

	// Count.
	memCount, err := memIdx.Count()
	s.Require().NoError(err)
	lazyCount, err := lazyIdx.Count()
	s.Require().NoError(err)
	s.Equal(memCount, lazyCount)

	// Entries: compare all fields.
	memIter, err := memIdx.Entries()
	s.Require().NoError(err)
	lazyIter, err := lazyIdx.Entries()
	s.Require().NoError(err)

	for {
		memEntry, memErr := memIter.Next()
		lazyEntry, lazyErr := lazyIter.Next()
		if memErr != nil {
			s.Require().ErrorIs(memErr, io.EOF, "unexpected memIter error")
			s.Require().ErrorIs(lazyErr, io.EOF, "unexpected lazyIter error")
			break
		}
		s.Require().NoError(lazyErr, "lazyIter error when memIter succeeded")
		s.Equal(memEntry.Hash, lazyEntry.Hash, "hash mismatch")
		s.Equal(memEntry.Offset, lazyEntry.Offset, "offset mismatch")
		s.Equal(memEntry.CRC32, lazyEntry.CRC32, "crc32 mismatch")

		// Per-entry: Contains, FindOffset, FindCRC32.
		memOk, err := memIdx.Contains(memEntry.Hash)
		s.Require().NoError(err)
		lazyOk, err := lazyIdx.Contains(memEntry.Hash)
		s.Require().NoError(err)
		s.Equal(memOk, lazyOk)

		memOff, err := memIdx.FindOffset(memEntry.Hash)
		s.Require().NoError(err)
		lazyOff, err := lazyIdx.FindOffset(memEntry.Hash)
		s.Require().NoError(err)
		s.Equal(memOff, lazyOff)

		memCRC, err := memIdx.FindCRC32(memEntry.Hash)
		s.Require().NoError(err)
		lazyCRC, err := lazyIdx.FindCRC32(memEntry.Hash)
		s.Require().NoError(err)
		s.Equal(memCRC, lazyCRC)

		// FindHash (reverse lookup).
		memHash, err := memIdx.FindHash(int64(memEntry.Offset))
		s.Require().NoError(err)
		lazyHash, err := lazyIdx.FindHash(int64(memEntry.Offset))
		s.Require().NoError(err)
		s.Equal(memHash, lazyHash)
	}
	s.Require().NoError(memIter.Close())
	s.Require().NoError(lazyIter.Close())

	// EntriesByOffset.
	memByOff, err := memIdx.EntriesByOffset()
	s.Require().NoError(err)
	lazyByOff, err := lazyIdx.EntriesByOffset()
	s.Require().NoError(err)

	for {
		me, mErr := memByOff.Next()
		le, lErr := lazyByOff.Next()
		if mErr != nil {
			s.Require().ErrorIs(mErr, io.EOF, "unexpected memByOff error")
			s.Require().ErrorIs(lErr, io.EOF, "unexpected lazyByOff error")
			break
		}
		s.Require().NoError(lErr, "lazyByOff error when memByOff succeeded")
		s.Equal(me.Hash, le.Hash)
		s.Equal(me.Offset, le.Offset)
	}
	s.Require().NoError(memByOff.Close())
	s.Require().NoError(lazyByOff.Close())
}

// TestDecodeLazyChecksumMismatch verifies that a corrupted checksum is rejected.
func (s *IdxfileSuite) TestDecodeLazyChecksumMismatch() {
	f := fixtures.Basic().One()
	idxBytes, err := io.ReadAll(f.Idx())
	s.Require().NoError(err)

	// Corrupt the last byte of the stored checksum.
	idxBytes[len(idxBytes)-1] ^= 0xFF

	openRev := func() (ReadAtCloser, error) { return nil, nil }
	packHash := plumbing.NewHash(f.PackfileHash)

	_, err = DecodeLazy(bytes.NewReader(idxBytes), hash.New(crypto.SHA1), openRev, packHash)
	s.Require().Error(err)
	s.ErrorIs(err, ErrMalformedIdxFile)
}

// TestDecodeLazyTruncated verifies that a truncated buffer is rejected.
func (s *IdxfileSuite) TestDecodeLazyTruncated() {
	openRev := func() (ReadAtCloser, error) { return nil, nil }
	packHash := plumbing.NewHash("0000000000000000000000000000000000000000")

	// Buffer shorter than one hash — too short.
	_, err := DecodeLazy(bytes.NewReader([]byte{0xAB, 0xCD}), hash.New(crypto.SHA1), openRev, packHash)
	s.Require().Error(err)
	s.ErrorIs(err, ErrMalformedIdxFile)
}
