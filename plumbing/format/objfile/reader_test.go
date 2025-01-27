package objfile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type SuiteReader struct {
	suite.Suite
}

func TestSuiteReader(t *testing.T) {
	suite.Run(t, new(SuiteReader))
}

func (s *SuiteReader) TestReadObjfile() {
	for k, fixture := range objfileFixtures {
		com := fmt.Sprintf("test %d: ", k)
		hash := plumbing.NewHash(fixture.hash)
		content, _ := base64.StdEncoding.DecodeString(fixture.content)
		data, _ := base64.StdEncoding.DecodeString(fixture.data)

		testReader(s.T(), bytes.NewReader(data), hash, fixture.t, content, com)
	}
}

func testReader(t *testing.T, source io.Reader, hash plumbing.Hash, o plumbing.ObjectType, content []byte, com string) {
	r, err := NewReader(source)
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
	_, err := NewReader(source)
	s.NotNil(err)
}

func (s *SuiteReader) TestReadGarbage() {
	source := bytes.NewReader([]byte("!@#$RO!@NROSADfinq@o#irn@oirfn"))
	_, err := NewReader(source)
	s.NotNil(err)
}

func (s *SuiteReader) TestReadCorruptZLib() {
	data, _ := base64.StdEncoding.DecodeString("eAFLysaalPUjBgAAAJsAHw")
	source := bytes.NewReader(data)
	r, err := NewReader(source)
	s.NoError(err)

	_, _, err = r.Header()
	s.NotNil(err)
}
