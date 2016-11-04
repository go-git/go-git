package git

import (
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/core"

	. "gopkg.in/check.v1"
)

type BlobsSuite struct {
	BaseSuite
}

var _ = Suite(&BlobsSuite{})

func (s *BlobsSuite) TestBlobHash(c *C) {
	o := &core.MemoryObject{}
	o.SetType(core.BlobObject)
	o.SetSize(3)

	writer, err := o.Writer()
	c.Assert(err, IsNil)
	defer func() { c.Assert(writer.Close(), IsNil) }()

	writer.Write([]byte{'F', 'O', 'O'})

	blob := &Blob{}
	c.Assert(blob.Decode(o), IsNil)

	c.Assert(blob.Size, Equals, int64(3))
	c.Assert(blob.Hash.String(), Equals, "d96c7efbfec2814ae0301ad054dc8d9fc416c9b5")

	reader, err := blob.Reader()
	c.Assert(err, IsNil)
	defer func() { c.Assert(reader.Close(), IsNil) }()

	data, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "FOO")
}

func (s *BlobsSuite) TestBlobDecodeEncodeIdempotent(c *C) {
	var objects []*core.MemoryObject
	for _, str := range []string{"foo", "foo\n"} {
		obj := &core.MemoryObject{}
		obj.Write([]byte(str))
		obj.SetType(core.BlobObject)
		obj.Hash()
		objects = append(objects, obj)
	}
	for _, object := range objects {
		blob := &Blob{}
		err := blob.Decode(object)
		c.Assert(err, IsNil)
		newObject := &core.MemoryObject{}
		err = blob.Encode(newObject)
		c.Assert(err, IsNil)
		newObject.Hash() // Ensure Hash is pre-computed before deep comparison
		c.Assert(newObject, DeepEquals, object)
	}
}

func (s *BlobsSuite) TestBlobIter(c *C) {
	iter, err := s.Repository.Blobs()
	c.Assert(err, IsNil)

	blobs := []*Blob{}
	iter.ForEach(func(b *Blob) error {
		blobs = append(blobs, b)
		return nil
	})

	c.Assert(len(blobs) > 0, Equals, true)
	iter.Close()

	iter, err = s.Repository.Blobs()
	c.Assert(err, IsNil)

	i := 0
	for {
		b, err := iter.Next()
		if err == io.EOF {
			break
		}

		c.Assert(err, IsNil)
		c.Assert(b, DeepEquals, blobs[i])
		i += 1
	}

	iter.Close()
}
