package objfile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
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
		testReader(s.T(), buffer, hash, fixture.t, content, format.SHA1, com)
	}
}

func testWriter(t *testing.T, dest io.Writer, hash plumbing.Hash, o plumbing.ObjectType, content []byte) {
	size := int64(len(content))
	w := NewWriter(dest, format.SHA1)

	err := w.WriteHeader(o, size)
	assert.NoError(t, err)

	written, err := io.Copy(w, bytes.NewReader(content))
	assert.NoError(t, err)
	assert.Equal(t, size, written)

	assert.Equal(t, hash, w.Hash())
	assert.NoError(t, w.Close())
}

func (s *SuiteWriter) TestWriteObjfileSHA256Hash() {
	content := []byte("hello sha256\n")
	hash := plumbing.NewHash("2928cdcdc8b78c930378ceba09ce9ca8b888fbfe1bffb2cceb42bdff9421cb52")
	buf := bytes.NewBuffer(nil)
	w := NewWriter(buf, format.SHA256)

	err := w.WriteHeader(plumbing.BlobObject, int64(len(content)))
	s.NoError(err)

	written, err := io.Copy(w, bytes.NewReader(content))
	s.NoError(err)
	s.Equal(int64(len(content)), written)

	s.Equal(hash, w.Hash())
	s.NoError(w.Close())
	testReader(s.T(), bytes.NewReader(buf.Bytes()), hash, plumbing.BlobObject, content, format.SHA256, "")
}

func (s *SuiteWriter) TestWriteOverflow() {
	buf := bytes.NewBuffer(nil)
	w := NewWriter(buf, format.SHA1)

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
	w := NewWriter(buf, format.SHA1)

	err := w.WriteHeader(plumbing.InvalidObject, 8)
	s.ErrorIs(err, plumbing.ErrInvalidType)
}

func (s *SuiteWriter) TestNewWriterInvalidSize() {
	buf := bytes.NewBuffer(nil)
	w := NewWriter(buf, format.SHA1)

	err := w.WriteHeader(plumbing.BlobObject, -1)
	s.ErrorIs(err, ErrNegativeSize)
	err = w.WriteHeader(plumbing.BlobObject, -1651860)
	s.ErrorIs(err, ErrNegativeSize)
}

func (s *SuiteWriter) TestWriteHeaderRejectsOversizeTypeBytes() {
	// Mirror the reader's MAX_HEADER_LEN-equivalent bound on the writer.
	// The largest valid type name today (e.g. "ofs-delta") plus the space,
	// a 19-digit size, and the NUL trailer is well under 32 bytes; this
	// test injects a longer type to confirm the guard fires.
	var buf bytes.Buffer
	w := NewWriter(&buf, format.SHA1)
	longType := bytes.Repeat([]byte("x"), maxHeaderLen)
	err := w.writeHeader(plumbing.BlobObject, longType, 0)
	s.ErrorIs(err, ErrHeaderTooLong)
}

func TestWriteHeaderBoundIsConstantForKnownTypes(t *testing.T) {
	t.Parallel()
	// Sanity guard: every currently-valid ObjectType plus a 19-digit size,
	// space, and trailing NUL must fit in maxHeaderLen. If anyone widens
	// ObjectType.Bytes() output past the cap this test breaks.
	for _, ot := range []plumbing.ObjectType{
		plumbing.BlobObject, plumbing.TreeObject,
		plumbing.CommitObject, plumbing.TagObject,
		plumbing.OFSDeltaObject, plumbing.REFDeltaObject,
	} {
		n := len(ot.Bytes()) + 1 + len(strconv.FormatInt(math.MaxInt64, 10)) + 1
		if n > maxHeaderLen {
			t.Fatalf("ObjectType %q produces %d-byte header, exceeds cap %d", ot, n, maxHeaderLen)
		}
	}
}
