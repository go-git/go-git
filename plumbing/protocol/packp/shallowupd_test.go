package packp

import (
	"bytes"

	"gopkg.in/src-d/go-git.v4/plumbing"

	. "gopkg.in/check.v1"
)

type ShallowUpdateSuite struct{}

var _ = Suite(&ShallowUpdateSuite{})

func (s *ShallowUpdateSuite) TestDecode(c *C) {
	raw := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	c.Assert(err, IsNil)

	plumbing.HashesSort(su.Shallows)

	c.Assert(su.Unshallows, HasLen, 0)
	c.Assert(su.Shallows, HasLen, 2)
	c.Assert(su.Shallows, DeepEquals, []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})
}

func (s *ShallowUpdateSuite) TestDecodeUnshallow(c *C) {
	raw := "" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	c.Assert(err, IsNil)

	plumbing.HashesSort(su.Unshallows)

	c.Assert(su.Shallows, HasLen, 0)
	c.Assert(su.Unshallows, HasLen, 2)
	c.Assert(su.Unshallows, DeepEquals, []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})
}

func (s *ShallowUpdateSuite) TestDecodeMalformed(c *C) {
	raw := "" +
		"0035unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	c.Assert(err, NotNil)
}
