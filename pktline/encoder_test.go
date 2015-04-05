package pktline

import (
	"io/ioutil"

	. "gopkg.in/check.v1"
)

type EncoderSuite struct{}

var _ = Suite(&EncoderSuite{})

func (s *EncoderSuite) TestEncode(c *C) {
	line, err := Encode([]byte("a\n"))
	c.Assert(err, IsNil)
	c.Assert(string(line), Equals, "0006a\n")
}

func (s *EncoderSuite) TestEncodeFromString(c *C) {
	line, err := EncodeFromString("a\n")
	c.Assert(err, IsNil)
	c.Assert(string(line), Equals, "0006a\n")
}

func (s *EncoderSuite) TestEncoder(c *C) {
	e := NewEncoder()
	e.AddLine("a")
	e.AddFlush()
	e.AddLine("b")

	r := e.GetReader()
	a, _ := ioutil.ReadAll(r)
	c.Assert(string(a), Equals, "0006a\n00000006b\n")
}
