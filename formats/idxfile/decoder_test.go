package idxfile

import (
	"fmt"
	"os"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type IdxfileSuite struct{}

var _ = Suite(&IdxfileSuite{})

func (s *IdxfileSuite) TestDecode(c *C) {
	f, err := os.Open("fixtures/git-fixture.idx")
	c.Assert(err, IsNil)

	d := NewDecoder(f)
	idx := &Idxfile{}
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	err = f.Close()
	c.Assert(err, IsNil)

	c.Assert(int(idx.ObjectCount), Equals, 31)
	c.Assert(idx.Entries, HasLen, 31)
	c.Assert(idx.Entries[0].Hash.String(), Equals,
		"1669dce138d9b841a518c64b10914d88f5e488ea")
	c.Assert(idx.Entries[0].Offset, Equals, uint64(615))

	c.Assert(fmt.Sprintf("%x", idx.IdxChecksum), Equals,
		"bba9b7a9895724819225a044c857d391bb9d61d9")
	c.Assert(fmt.Sprintf("%x", idx.PackfileChecksum), Equals,
		"54bb61360ab2dad1a3e344a8cd3f82b848518cba")

}
