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

func (s *SuiteReader) TestHeaderReadsExactMaxLength() {
	// "blob " (5) + 26 size digits + NUL = 32 bytes exactly. The cap is on
	// excess, not equality, so the size field is parsed. Canonical Git
	// rejects it there, as the size is not in canonical decimal form.
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

	_, _, err = r.Header()
	s.ErrorIs(err, ErrHeader)
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

func (s *SuiteReader) TestHeaderRejectsNonCanonicalSize() {
	for _, size := range []string{"", "-1", "+1", "01", "00", " 1", "1 ", "0x1", "1e3"} {
		s.Run(fmt.Sprintf("%q", size), func() {
			payload := []byte("blob " + size + "\x00")
			r, err := NewReader(bytes.NewReader(compress(s.T(), payload)), format.SHA1)
			s.Require().NoError(err)
			defer r.Close()

			_, _, err = r.Header()
			s.ErrorIs(err, ErrHeader)
		})
	}
}

func (s *SuiteReader) TestReadRejectsTrailingData() {
	data := append(compress(s.T(), []byte("blob 5\x00hello")), 'x')
	r, err := NewReader(bytes.NewReader(data), format.SHA1)
	s.Require().NoError(err)
	defer r.Close()

	_, _, err = r.Header()
	s.Require().NoError(err)

	got, err := io.ReadAll(r)
	s.ErrorIs(err, ErrTrailingData)
	s.Equal([]byte("hello"), got)
}

func (s *SuiteReader) TestReadBodyMatchesDeclaredSize() {
	tests := []struct {
		name    string
		payload []byte
		want    []byte
		wantErr error
	}{
		{
			name:    "exact",
			payload: []byte("blob 5\x00hello"),
			want:    []byte("hello"),
		},
		{
			name:    "empty",
			payload: []byte("tree 0\x00"),
			want:    []byte{},
		},
		{
			name:    "longer than declared",
			payload: append([]byte("blob 1\x00"), bytes.Repeat([]byte{'A'}, 64<<10)...),
			want:    []byte("A"),
			wantErr: ErrOverflow,
		},
		{
			name:    "non-empty body for empty object",
			payload: []byte("tree 0\x00x"),
			want:    []byte{},
			wantErr: ErrOverflow,
		},
		{
			name:    "shorter than declared",
			payload: []byte("blob 5\x00hi"),
			want:    []byte("hi"),
			wantErr: io.ErrUnexpectedEOF,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			r, err := NewReader(bytes.NewReader(compress(s.T(), tt.payload)), format.SHA1)
			s.Require().NoError(err)
			defer r.Close()

			_, _, err = r.Header()
			s.Require().NoError(err)

			got, err := io.ReadAll(r)
			if tt.wantErr != nil {
				s.ErrorIs(err, tt.wantErr)
			} else {
				s.NoError(err)
			}
			s.Equal(tt.want, got)

			// The result is sticky across further reads.
			var buf [16]byte
			n, err := r.Read(buf[:])
			s.Equal(0, n)
			if tt.wantErr != nil {
				s.ErrorIs(err, tt.wantErr)
			} else {
				s.ErrorIs(err, io.EOF)
			}
		})
	}
}

func compress(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, err := w.Write(payload)
	assert.NoError(t, err)
	assert.NoError(t, w.Close())
	return buf.Bytes()
}

func (s *SuiteReader) TestUseAfterClose() {
	newReader := func() *Reader {
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		_, err := w.Write([]byte("blob 5\x00hello"))
		s.Require().NoError(err)
		s.Require().NoError(w.Close())

		r, err := NewReader(&buf, format.SHA1)
		s.Require().NoError(err)
		return r
	}

	s.Run("Header", func() {
		r := newReader()
		s.Require().NoError(r.Close())

		_, _, err := r.Header()
		s.ErrorIs(err, ErrClosed)
	})

	s.Run("Read", func() {
		r := newReader()
		_, _, err := r.Header()
		s.Require().NoError(err)
		s.Require().NoError(r.Close())

		_, err = r.Read(make([]byte, 5))
		s.ErrorIs(err, ErrClosed)
	})
}
