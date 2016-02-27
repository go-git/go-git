package objfile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v3/core"
)

type SuiteWriter struct{}

var _ = Suite(&SuiteWriter{})

func (s *SuiteWriter) TestWriteObjfile(c *C) {
	for k, fixture := range objfileFixtures {
		comment := fmt.Sprintf("test %d: ", k)
		hash := core.NewHash(fixture.hash)
		content, _ := base64.StdEncoding.DecodeString(fixture.content)
		buffer := new(bytes.Buffer)

		// Write the data out to the buffer
		testWriter(c, buffer, hash, fixture.t, content, comment)

		// Read the data back in from the buffer to be sure it matches
		testReader(c, buffer, hash, fixture.t, content, comment)
	}
}

func testWriter(c *C, dest io.Writer, hash core.Hash, typ core.ObjectType, content []byte, comment string) {
	length := int64(len(content))
	w, err := NewWriter(dest, typ, length)
	c.Assert(err, IsNil)
	c.Assert(w.Type(), Equals, typ)
	c.Assert(w.Size(), Equals, length)
	written, err := io.Copy(w, bytes.NewReader(content))
	c.Assert(err, IsNil)
	c.Assert(written, Equals, length)
	c.Assert(w.Hash(), Equals, hash) // Test Hash() before close
	c.Assert(w.Close(), IsNil)
	c.Assert(w.Hash(), Equals, hash) // Test Hash() after close
}

func (s *SuiteWriter) TestWriteOverflow(c *C) {
	w, err := NewWriter(new(bytes.Buffer), core.BlobObject, 8)
	c.Assert(err, IsNil)
	_, err = w.Write([]byte("1234"))
	c.Assert(err, IsNil)
	_, err = w.Write([]byte("56789"))
	c.Assert(err, Equals, ErrOverflow)
}
