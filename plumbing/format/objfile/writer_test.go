package objfile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

type SuiteWriter struct {
	suite.Suite
}

func TestSuiteWriter(t *testing.T) {
	t.Parallel()
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

func (s *SuiteWriter) TestNewWriterWithFormat() {
	buf := bytes.NewBuffer(nil)
	content := []byte("sha256 content")
	w := NewWriterWithFormat(buf, formatcfg.SHA256)

	err := w.WriteHeader(plumbing.BlobObject, int64(len(content)))
	s.NoError(err)

	_, err = io.Copy(w, bytes.NewReader(content))
	s.NoError(err)

	hasher := plumbing.NewHasher(formatcfg.SHA256, plumbing.BlobObject, int64(len(content)))
	_, err = hasher.Write(content)
	s.NoError(err)
	expected := hasher.Sum()
	s.Equal(expected, w.Hash())
	s.NoError(w.Close())
}
