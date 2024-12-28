package idxfile_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	. "github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type IdxfileFixtureSuite struct {
	fixtures.Suite
}

type IdxfileSuite struct {
	suite.Suite
	IdxfileFixtureSuite
}

func TestIdxfileSuite(t *testing.T) {
	suite.Run(t, new(IdxfileSuite))
}

func (s *IdxfileSuite) TestDecode() {
	f := fixtures.Basic().One()

	d := NewDecoder(f.Idx())
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

	s.Equal("fb794f1ec720b9bc8e43257451bd99c4be6fa1c9", fmt.Sprintf("%x", idx.IdxChecksum))
	s.Equal(f.PackfileHash, fmt.Sprintf("%x", idx.PackfileChecksum))
}

func (s *IdxfileSuite) TestDecode64bitsOffsets() {
	f := bytes.NewBufferString(fixtureLarge4GB)

	idx := new(MemoryIndex)

	d := NewDecoder(base64.NewDecoder(base64.StdEncoding, f))
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
	fixture, err := io.ReadAll(f.Idx())
	if err != nil {
		b.Errorf("unexpected error reading idx file: %s", err)
	}

	defer fixtures.Clean()

	for i := 0; i < b.N; i++ {
		f := bytes.NewBuffer(fixture)
		idx := new(MemoryIndex)
		d := NewDecoder(f)
		if err := d.Decode(idx); err != nil {
			b.Errorf("unexpected error decoding: %s", err)
		}
	}
}
