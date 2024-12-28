package objfile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type SuiteWriter struct {
	suite.Suite
}

func TestSuiteWriter(t *testing.T) {
	suite.Run(t, new(SuiteWriter))
}

func (s *SuiteWriter) TestWriteObjfile() {
	for k, fixture := range objfileFixtures {
		buffer := bytes.NewBuffer(nil)

		com := fmt.Sprintf("test %d: ", k)
		hash := plumbing.NewHash(fixture.hash)
		content, _ := base64.StdEncoding.DecodeString(fixture.content)

		// Write the data out to the buffer
		testWriter(s.T(), buffer, hash, fixture.t, content)

		// Read the data back in from the buffer to be sure it matches
		testReader(s.T(), buffer, hash, fixture.t, content, com)
	}
}

func testWriter(t *testing.T, dest io.Writer, hash plumbing.Hash, o plumbing.ObjectType, content []byte) {
	size := int64(len(content))
	w := NewWriter(dest)

	err := w.WriteHeader(o, size)
	assert.NoError(t, err)

	written, err := io.Copy(w, bytes.NewReader(content))
	assert.NoError(t, err)
	assert.Equal(t, size, written)

	assert.Equal(t, hash, w.Hash())
	assert.NoError(t, w.Close())
}

func (s *SuiteWriter) TestWriteOverflow() {
	buf := bytes.NewBuffer(nil)
	w := NewWriter(buf)

	err := w.WriteHeader(plumbing.BlobObject, 8)
	s.NoError(err)

	n, err := w.Write([]byte("1234"))
	s.NoError(err)
	s.Equal(4, n)

	n, err = w.Write([]byte("56789"))
	s.ErrorIs(err, ErrOverflow)
	s.Equal(4, n)
}

func (s *SuiteWriter) TestNewWriterInvalidType() {
	buf := bytes.NewBuffer(nil)
	w := NewWriter(buf)

	err := w.WriteHeader(plumbing.InvalidObject, 8)
	s.ErrorIs(err, plumbing.ErrInvalidType)
}

func (s *SuiteWriter) TestNewWriterInvalidSize() {
	buf := bytes.NewBuffer(nil)
	w := NewWriter(buf)

	err := w.WriteHeader(plumbing.BlobObject, -1)
	s.ErrorIs(err, ErrNegativeSize)
	err = w.WriteHeader(plumbing.BlobObject, -1651860)
	s.ErrorIs(err, ErrNegativeSize)
}
