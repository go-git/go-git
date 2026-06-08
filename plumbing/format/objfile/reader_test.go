package objfile

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

type SuiteReader struct {
	suite.Suite
}

func TestSuiteReader(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteReader))
}

func (s *SuiteReader) TestReadObjfile() {
	tests := []struct {
		name         string
		objectFormat format.ObjectFormat
	}{
		{name: "sha1", objectFormat: format.SHA1},
		{name: "sha256", objectFormat: format.SHA256},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			for k, fixture := range objfileFixtures {
				com := fmt.Sprintf("%s test %d: ", tt.name, k)
				content, _ := base64.StdEncoding.DecodeString(fixture.content)
				data, _ := base64.StdEncoding.DecodeString(fixture.data)
				hash := readerFixtureHash(fixture, content, tt.objectFormat)

				testReader(s.T(), bytes.NewReader(data), hash, fixture.t, content, tt.objectFormat, com)
			}
		})
	}
}

func readerFixtureHash(
	fixture objfileFixture,
	content []byte,
	objectFormat format.ObjectFormat,
) plumbing.Hash {
	if objectFormat == format.SHA1 {
		return plumbing.NewHash(fixture.hash)
	}

	hasher := plumbing.NewHasher(objectFormat, fixture.t, int64(len(content)))
	_, _ = hasher.Write(content)
	return hasher.Sum()
}

func testReader(
	t *testing.T,
	source io.Reader,
	hash plumbing.Hash,
	o plumbing.ObjectType,
	content []byte,
	objectFormat format.ObjectFormat,
	_ string,
) {
	r, err := NewReader(source, objectFormat)
	assert.NoError(t, err)

	typ, size, err := r.Header()
	assert.NoError(t, err)
	assert.Equal(t, typ, o)
	assert.Len(t, content, int(size))

	rc, err := io.ReadAll(r)
	assert.NoError(t, err)
	assert.Equal(t, content, rc, fmt.Sprintf("content=%s, expected=%s", base64.StdEncoding.EncodeToString(rc), base64.StdEncoding.EncodeToString(content)))

	assert.Equal(t, hash, r.Hash()) // Test Hash() before close
	assert.NoError(t, r.Close())
}

func (s *SuiteReader) TestReadEmptyObjfile() {
	source := bytes.NewReader([]byte{})
	_, err := NewReader(source, format.SHA1)
	s.NotNil(err)
}

func (s *SuiteReader) TestReadGarbage() {
	source := bytes.NewReader([]byte("!@#$RO!@NROSADfinq@o#irn@oirfn"))
	_, err := NewReader(source, format.SHA1)
	s.NotNil(err)
}

func (s *SuiteReader) TestReadCorruptZLib() {
	data, _ := base64.StdEncoding.DecodeString("eAFLysaalPUjBgAAAJsAHw")
	source := bytes.NewReader(data)
	r, err := NewReader(source, format.SHA1)
	s.NoError(err)

	_, _, err = r.Header()
	s.NotNil(err)
}

func (s *SuiteReader) TestReaderReadBeforeHeader() {
	tests := []struct {
		name         string
		objectFormat format.ObjectFormat
		wantSize     int
	}{
		{name: "sha1", objectFormat: format.SHA1, wantSize: format.SHA1Size},
		{name: "sha256", objectFormat: format.SHA256, wantSize: format.SHA256Size},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			data, _ := base64.StdEncoding.DecodeString(objfileFixtures[0].data)
			source := bytes.NewReader(data)

			r, err := NewReader(source, tt.objectFormat)
			s.NoError(err)
			defer r.Close()

			var buf [16]byte
			n, err := r.Read(buf[:])
			s.ErrorIs(err, ErrHeaderNotRead)
			s.Equal(0, n)

			// The zero hash must carry the Reader's configured object
			// format so callers that serialise it emit the right number
			// of bytes.
			h := r.Hash()
			s.True(h.IsZero())
			s.Equal(tt.wantSize, h.Size())
		})
	}
}

func (s *SuiteReader) TestHeaderRejectsOverlongInflatedBytes() {
	// The inflated stream contains 1 KiB of payload with neither the type
	// delimiter (' ') nor the trailing NUL inside the first 32 bytes. The
	// reader must refuse to consume the full payload looking for a
	// delimiter, matching canonical Git's MAX_HEADER_LEN.
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, err := w.Write(bytes.Repeat([]byte{'b'}, 1024))
	s.Require().NoError(err)
	s.Require().NoError(w.Close())

	r, err := NewReader(&buf, format.SHA1)
	s.Require().NoError(err)
	defer r.Close()

	_, _, err = r.Header()
	s.ErrorIs(err, ErrHeaderTooLong)
}

func (s *SuiteReader) TestHeaderRejectsOverlongSizeField() {
	// Type field fits, but the size field has no NUL within budget.
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	payload := append([]byte("blob "), bytes.Repeat([]byte{'0'}, 1024)...)
	_, err := w.Write(payload)
	s.Require().NoError(err)
	s.Require().NoError(w.Close())

	r, err := NewReader(&buf, format.SHA1)
	s.Require().NoError(err)
	defer r.Close()

	_, _, err = r.Header()
	s.ErrorIs(err, ErrHeaderTooLong)
}

func (s *SuiteReader) TestHeaderAcceptsExactMaxLength() {
	// "blob " (5) + 26 size digits + NUL = 32 bytes exactly. Canonical Git
	// accepts this; the cap is on excess, not equality.
	payload := append([]byte("blob "), bytes.Repeat([]byte{'0'}, 26)...)
	payload = append(payload, 0)
	s.Require().Len(payload, 32)

	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, err := w.Write(payload)
	s.Require().NoError(err)
	s.Require().NoError(w.Close())

	r, err := NewReader(&buf, format.SHA1)
	s.Require().NoError(err)
	defer r.Close()

	t, size, err := r.Header()
	s.Require().NoError(err)
	s.Equal(plumbing.BlobObject, t)
	s.Equal(int64(0), size)
}

func (s *SuiteReader) TestHeaderRejectsOneByteOverMaxLength() {
	// 33-byte header: cap exceeded by exactly one byte.
	payload := append([]byte("blob "), bytes.Repeat([]byte{'0'}, 27)...)
	payload = append(payload, 0)
	s.Require().Len(payload, 33)

	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, err := w.Write(payload)
	s.Require().NoError(err)
	s.Require().NoError(w.Close())

	r, err := NewReader(&buf, format.SHA1)
	s.Require().NoError(err)
	defer r.Close()

	_, _, err = r.Header()
	s.ErrorIs(err, ErrHeaderTooLong)
}

func (s *SuiteReader) TestReaderReadAfterHeaderError() {
	// This zlib stream decompresses to bytes that do not form a valid
	// loose-object header, so Header() returns an error.
	data, _ := base64.StdEncoding.DecodeString("eAFLysaalPUjBgAAAJsAHw")
	source := bytes.NewReader(data)

	r, err := NewReader(source, format.SHA1)
	s.NoError(err)
	defer r.Close()

	_, _, err = r.Header()
	s.Error(err)

	// Read must return an error rather than accessing uninitialised state.
	var buf [16]byte
	n, readErr := r.Read(buf[:])
	s.Error(readErr)
	s.Equal(0, n)
}
