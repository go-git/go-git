package idxfile

import (
	"bytes"
	"io"
	"os"

	. "gopkg.in/check.v1"
)

func (s *IdxfileSuite) TestEncode(c *C) {
	for i, path := range [...]string{
		"fixtures/git-fixture.idx",
		"../packfile/fixtures/spinnaker-spinnaker.idx",
	} {
		com := Commentf("subtest %d: path = %s", i, path)

		exp, idx, err := decode(path)
		c.Assert(err, IsNil, com)

		obt := new(bytes.Buffer)
		e := NewEncoder(obt)
		size, err := e.Encode(idx)
		c.Assert(err, IsNil, com)

		c.Assert(size, Equals, exp.Len(), com)
		c.Assert(obt, DeepEquals, exp, com)
	}
}

func decode(path string) (*bytes.Buffer, *Idxfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	cont := new(bytes.Buffer)
	tee := io.TeeReader(f, cont)

	d := NewDecoder(tee)
	idx := &Idxfile{}
	if err = d.Decode(idx); err != nil {
		return nil, nil, err
	}

	return cont, idx, f.Close()
}
