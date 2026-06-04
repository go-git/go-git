package plumbing

import (
	"io"
	"testing"

	"github.com/stretchr/testify/suite"
)

type MemoryObjectSuite struct {
	suite.Suite
}

func TestMemoryObjectSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(MemoryObjectSuite))
}

func (s *MemoryObjectSuite) TestHash() {
	o := &MemoryObject{}
	o.SetType(BlobObject)
	o.SetSize(14)

	_, err := o.Write([]byte("Hello, World!\n"))
	s.NoError(err)

	s.Equal("8ab686eafeb1f44702738c8b0f24f2567c36da6d", o.Hash().String())

	o.SetType(CommitObject)
	s.Equal("8ab686eafeb1f44702738c8b0f24f2567c36da6d", o.Hash().String())
}

func (s *MemoryObjectSuite) TestHashNotFilled() {
	o := &MemoryObject{}
	o.SetType(BlobObject)
	o.SetSize(14)

	s.Equal(ZeroHash, o.Hash())
}

func (s *MemoryObjectSuite) TestType() {
	o := &MemoryObject{}
	o.SetType(BlobObject)
	s.Equal(BlobObject, o.Type())
}

func (s *MemoryObjectSuite) TestSize() {
	o := &MemoryObject{}
	o.SetSize(42)
	s.Equal(int64(42), o.Size())
}

func (s *MemoryObjectSuite) TestReader() {
	o := &MemoryObject{cont: []byte("foo")}

	reader, err := o.Reader()
	s.NoError(err)
	defer func() { s.Nil(reader.Close()) }()

	b, err := io.ReadAll(reader)
	s.NoError(err)
	s.Equal([]byte("foo"), b)
}

func (s *MemoryObjectSuite) TestSeekableReader() {
	const pageSize = 4096
	const payload = "foo"
	content := make([]byte, pageSize+len(payload))
	copy(content[pageSize:], []byte(payload))

	o := &MemoryObject{cont: content}

	reader, err := o.Reader()
	s.NoError(err)
	defer func() { s.Nil(reader.Close()) }()

	rs, ok := reader.(io.ReadSeeker)
	s.True(ok)

	_, err = rs.Seek(pageSize, io.SeekStart)
	s.NoError(err)

	b, err := io.ReadAll(rs)
	s.NoError(err)
	s.Equal([]byte(payload), b)

	// Check that our Reader isn't also accidentally writable
	_, ok = reader.(io.WriteSeeker)
	s.False(ok)
}

func (s *MemoryObjectSuite) TestWriter() {
	o := &MemoryObject{}

	writer, err := o.Writer()
	s.NoError(err)
	defer func() { s.Nil(writer.Close()) }()

	n, err := writer.Write([]byte("foo"))
	s.NoError(err)
	s.Equal(3, n)

	s.Equal([]byte("foo"), o.cont)
}
