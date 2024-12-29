package object

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/suite"
)

type BlobsSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func TestBlobsSuite(t *testing.T) {
	suite.Run(t, new(BlobsSuite))
}

func (s *BlobsSuite) SetupSuite() {
	s.BaseObjectsSuite.SetupSuite(s.T())
}

func (s *BlobsSuite) TestBlobHash() {
	o := &plumbing.MemoryObject{}
	o.SetType(plumbing.BlobObject)
	o.SetSize(3)

	writer, err := o.Writer()
	s.NoError(err)
	defer func() { s.Nil(writer.Close()) }()

	writer.Write([]byte{'F', 'O', 'O'})

	blob := &Blob{}
	s.Nil(blob.Decode(o))

	s.Equal(int64(3), blob.Size)
	s.Equal("d96c7efbfec2814ae0301ad054dc8d9fc416c9b5", blob.Hash.String())

	reader, err := blob.Reader()
	s.NoError(err)
	defer func() { s.Nil(reader.Close()) }()

	data, err := io.ReadAll(reader)
	s.NoError(err)
	s.Equal("FOO", string(data))
}

func (s *BlobsSuite) TestBlobDecodeEncodeIdempotent() {
	var objects []*plumbing.MemoryObject
	for _, str := range []string{"foo", "foo\n"} {
		obj := &plumbing.MemoryObject{}
		obj.Write([]byte(str))
		obj.SetType(plumbing.BlobObject)
		obj.Hash()
		objects = append(objects, obj)
	}
	for _, object := range objects {
		blob := &Blob{}
		err := blob.Decode(object)
		s.NoError(err)
		newObject := &plumbing.MemoryObject{}
		err = blob.Encode(newObject)
		s.NoError(err)
		newObject.Hash() // Ensure Hash is pre-computed before deep comparison
		s.Equal(object, newObject)
	}
}

func (s *BlobsSuite) TestBlobIter() {
	encIter, err := s.Storer.IterEncodedObjects(plumbing.BlobObject)
	s.NoError(err)
	iter := NewBlobIter(s.Storer, encIter)

	blobs := []*Blob{}
	iter.ForEach(func(b *Blob) error {
		blobs = append(blobs, b)
		return nil
	})

	s.True(len(blobs) > 0)
	iter.Close()

	encIter, err = s.Storer.IterEncodedObjects(plumbing.BlobObject)
	s.NoError(err)
	iter = NewBlobIter(s.Storer, encIter)

	i := 0
	for {
		b, err := iter.Next()
		if err == io.EOF {
			break
		}

		s.NoError(err)
		s.Equal(blobs[i].ID(), b.ID())
		s.Equal(blobs[i].Size, b.Size)
		s.Equal(blobs[i].Type(), b.Type())

		r1, err := b.Reader()
		s.NoError(err)

		b1, err := io.ReadAll(r1)
		s.NoError(err)
		s.Nil(r1.Close())

		r2, err := blobs[i].Reader()
		s.NoError(err)

		b2, err := io.ReadAll(r2)
		s.NoError(err)
		s.Nil(r2.Close())

		s.Equal(0, bytes.Compare(b1, b2))
		i++
	}

	iter.Close()
}
