package idxfile

import (
	"bytes"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/storage/memory"
)

func Test(t *testing.T) { TestingT(t) }

type IdxfileSuite struct {
	fixtures.Suite
}

var _ = Suite(&IdxfileSuite{})

func (s *IdxfileSuite) TestDecode(c *C) {
	f := fixtures.Basic().One()

	d := NewDecoder(f.Idx())
	idx := &Idxfile{}
	err := d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Entries, HasLen, 31)
	c.Assert(idx.Entries[0].Hash.String(), Equals, "1669dce138d9b841a518c64b10914d88f5e488ea")
	c.Assert(idx.Entries[0].Offset, Equals, uint64(615))
	c.Assert(idx.Entries[0].CRC32, Equals, uint32(3645019190))

	c.Assert(fmt.Sprintf("%x", idx.IdxChecksum), Equals, "fb794f1ec720b9bc8e43257451bd99c4be6fa1c9")
	c.Assert(fmt.Sprintf("%x", idx.PackfileChecksum), Equals, f.PackfileHash.String())
}

func (s *IdxfileSuite) TestDecodeCRCs(c *C) {
	f := fixtures.Basic().ByTag("ofs-delta").One()

	scanner := packfile.NewScanner(f.Packfile())
	storage := memory.NewStorage()

	pd, err := packfile.NewDecoder(scanner, storage)
	c.Assert(err, IsNil)
	_, err = pd.Decode()
	c.Assert(err, IsNil)

	i := &Idxfile{Version: VersionSupported}

	offsets := pd.Offsets()
	for h, crc := range pd.CRCs() {
		i.Add(h, uint64(offsets[h]), crc)
	}

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	_, err = e.Encode(i)
	c.Assert(err, IsNil)

	idx := &Idxfile{}

	d := NewDecoder(buf)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Entries, DeepEquals, i.Entries)
}
