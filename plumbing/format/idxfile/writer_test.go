package idxfile_test

import (
	"bytes"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing/format/idxfile"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git-fixtures.v3"
)

type IndexSuite struct {
	fixtures.Suite
}

var _ = Suite(&IndexSuite{})

func (s *IndexSuite) TestIndexWriter(c *C) {
	f := fixtures.Basic().One()
	scanner := packfile.NewScanner(f.Packfile())

	obs := new(idxfile.Writer)
	parser := packfile.NewParser(scanner, obs)

	_, err := parser.Parse()
	c.Assert(err, IsNil)

	idx, err := obs.Index()
	c.Assert(err, IsNil)

	idxFile := f.Idx()
	expected, err := ioutil.ReadAll(idxFile)
	c.Assert(err, IsNil)
	idxFile.Close()

	buf := new(bytes.Buffer)
	encoder := idxfile.NewEncoder(buf)
	n, err := encoder.Encode(idx)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len(expected))

	c.Assert(buf.Bytes(), DeepEquals, expected)
}
