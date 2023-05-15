package idxfile_test

import (
	"bytes"
	"io"

	. "github.com/go-git/go-git/v5/plumbing/format/idxfile"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func (s *IdxfileSuite) TestDecodeEncode(c *C) {
	fixtures.ByTag("packfile").Test(c, func(f *fixtures.Fixture) {
		expected, err := io.ReadAll(f.Idx())
		c.Assert(err, IsNil)

		idx := new(MemoryIndex)
		d := NewDecoder(bytes.NewBuffer(expected))
		err = d.Decode(idx)
		c.Assert(err, IsNil)

		result := bytes.NewBuffer(nil)
		e := NewEncoder(result)
		size, err := e.Encode(idx)
		c.Assert(err, IsNil)

		c.Assert(size, Equals, len(expected))
		c.Assert(result.Bytes(), DeepEquals, expected)
	})
}
