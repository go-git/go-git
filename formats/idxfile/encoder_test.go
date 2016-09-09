package idxfile

import (
	"bytes"
	"io/ioutil"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/fixtures"
)

func (s *IdxfileSuite) SetUpSuite(c *C) {
	fixtures.RootFolder = "../../fixtures"
}

func (s *IdxfileSuite) TestEncode(c *C) {
	expected := &Idxfile{}
	expected.Add(core.NewHash("4bfc730165c370df4a012afbb45ba3f9c332c0d4"), 82, 82)
	expected.Add(core.NewHash("8fa2238efdae08d83c12ee176fae65ff7c99af46"), 42, 42)

	buf := bytes.NewBuffer(nil)
	e := NewEncoder(buf)
	_, err := e.Encode(expected)
	c.Assert(err, IsNil)

	idx := &Idxfile{}
	d := NewDecoder(buf)
	err = d.Decode(idx)
	c.Assert(err, IsNil)

	c.Assert(idx.Entries, DeepEquals, expected.Entries)
}

func (s *IdxfileSuite) TestDecodeEncode(c *C) {
	fixtures.ByTag("packfile").Test(c, func(f *fixtures.Fixture) {
		expected, err := ioutil.ReadAll(f.Idx())
		c.Assert(err, IsNil)

		idx := &Idxfile{}
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
