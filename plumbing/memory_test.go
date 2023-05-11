package plumbing

import (
	"io"

	. "gopkg.in/check.v1"
)

type MemoryObjectSuite struct{}

var _ = Suite(&MemoryObjectSuite{})

func (s *MemoryObjectSuite) TestHash(c *C) {
	o := &MemoryObject{}
	o.SetType(BlobObject)
	o.SetSize(14)

	_, err := o.Write([]byte("Hello, World!\n"))
	c.Assert(err, IsNil)

	c.Assert(o.Hash().String(), Equals, "8ab686eafeb1f44702738c8b0f24f2567c36da6d")

	o.SetType(CommitObject)
	c.Assert(o.Hash().String(), Equals, "8ab686eafeb1f44702738c8b0f24f2567c36da6d")
}

func (s *MemoryObjectSuite) TestHashNotFilled(c *C) {
	o := &MemoryObject{}
	o.SetType(BlobObject)
	o.SetSize(14)

	c.Assert(o.Hash(), Equals, ZeroHash)
}

func (s *MemoryObjectSuite) TestType(c *C) {
	o := &MemoryObject{}
	o.SetType(BlobObject)
	c.Assert(o.Type(), Equals, BlobObject)
}

func (s *MemoryObjectSuite) TestSize(c *C) {
	o := &MemoryObject{}
	o.SetSize(42)
	c.Assert(o.Size(), Equals, int64(42))
}

func (s *MemoryObjectSuite) TestReader(c *C) {
	o := &MemoryObject{cont: []byte("foo")}

	reader, err := o.Reader()
	c.Assert(err, IsNil)
	defer func() { c.Assert(reader.Close(), IsNil) }()

	b, err := io.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte("foo"))
}

func (s *MemoryObjectSuite) TestSeekableReader(c *C) {
	const pageSize = 4096
	const payload = "foo"
	content := make([]byte, pageSize+len(payload))
	copy(content[pageSize:], []byte(payload))

	o := &MemoryObject{cont: content}

	reader, err := o.Reader()
	c.Assert(err, IsNil)
	defer func() { c.Assert(reader.Close(), IsNil) }()

	rs, ok := reader.(io.ReadSeeker)
	c.Assert(ok, Equals, true)

	_, err = rs.Seek(pageSize, io.SeekStart)
	c.Assert(err, IsNil)

	b, err := io.ReadAll(rs)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte(payload))

	// Check that our Reader isn't also accidentally writable
	_, ok = reader.(io.WriteSeeker)
	c.Assert(ok, Equals, false)
}

func (s *MemoryObjectSuite) TestWriter(c *C) {
	o := &MemoryObject{}

	writer, err := o.Writer()
	c.Assert(err, IsNil)
	defer func() { c.Assert(writer.Close(), IsNil) }()

	n, err := writer.Write([]byte("foo"))
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)

	c.Assert(o.cont, DeepEquals, []byte("foo"))
}
