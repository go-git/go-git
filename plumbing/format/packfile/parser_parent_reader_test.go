package packfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

// sectionReaderStorage serves every object through an io.SectionReader,
// which, unlike plumbing.MemoryObject's reader, does not implement
// io.WriterTo.
type sectionReaderStorage struct {
	storer.EncodedObjectStorer
}

func (s sectionReaderStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := s.EncodedObjectStorer.EncodedObject(t, h)
	if err != nil {
		return nil, err
	}

	return sectionReaderObject{EncodedObject: obj}, nil
}

type sectionReaderObject struct {
	plumbing.EncodedObject
}

func (o sectionReaderObject) Reader() (io.ReadCloser, error) {
	r, err := o.EncodedObject.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(io.NewSectionReader(bytes.NewReader(b), 0, int64(len(b)))), nil
}

// TestParentReaderDoesNotOverallocateContent is a regression test for
// https://github.com/go-git/go-git/issues/2574.
//
// parentReader grows the destination buffer to the parent's size and then
// copies the object stored in the storage into it. When the storage reader
// does not implement io.WriterTo, io.CopyBuffer falls back to
// bytes.Buffer.ReadFrom, which grows the buffer by MinRead before every
// read. Because the buffer is already exactly full, the read that observes
// EOF reallocates it to about twice its size and copies the parent into the
// new allocation.
func TestParentReaderDoesNotOverallocateContent(t *testing.T) {
	t.Parallel()

	const size = 4 << 20
	content := bytes.Repeat([]byte("0123456789abcdef"), size/16)

	base := memory.NewStorage()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.BlobObject)
	_, err := obj.Write(content)
	require.NoError(t, err)
	hash, err := base.SetEncodedObject(obj)
	require.NoError(t, err)

	p := NewParser(bytes.NewReader(nil), WithStorage(sectionReaderStorage{base}))

	served, err := p.storage.EncodedObject(plumbing.BlobObject, hash)
	require.NoError(t, err)
	servedReader, err := served.Reader()
	require.NoError(t, err)
	_, isWriterTo := servedReader.(io.WriterTo)
	require.False(t, isWriterTo, "test premise: the storage reader must not implement io.WriterTo")
	require.NoError(t, servedReader.Close())

	// Reproduce the state parentReader leaves behind after
	// Grow(growHint(parent.Size)): an empty buffer whose capacity is
	// exactly the parent's size.
	parent := &ObjectHeader{
		Hash:    hash,
		Type:    plumbing.BlobObject,
		Size:    size,
		content: bytes.NewBuffer(make([]byte, 0, size)),
	}

	_, n, err := p.parentReader(parent)
	require.NoError(t, err)
	require.EqualValues(t, size, n)
	require.Equal(t, size, parent.content.Len())
	assert.True(t, bytes.Equal(content, parent.content.Bytes()),
		"parent content must be copied verbatim")

	// Copying must fill the pre-grown buffer in place. Doubling the
	// capacity just to observe EOF is the memory regression.
	assert.Equal(t, size, parent.content.Cap(),
		"copying the parent must not reallocate the exactly-sized buffer")
}
