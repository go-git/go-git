package packp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type ShallowUpdateSuite struct {
	suite.Suite
}

func TestShallowUpdateSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ShallowUpdateSuite))
}

func (s *ShallowUpdateSuite) TestDecodeWithLF() {
	raw := "" +
		"0035shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0035shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NoError(err)

	plumbing.HashesSort(su.Shallows)

	s.Len(su.Unshallows, 0)
	s.Len(su.Shallows, 2)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}, su.Shallows)
}

func (s *ShallowUpdateSuite) TestDecode() {
	raw := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NoError(err)

	plumbing.HashesSort(su.Shallows)

	s.Len(su.Unshallows, 0)
	s.Len(su.Shallows, 2)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}, su.Shallows)
}

func (s *ShallowUpdateSuite) TestDecodeUnshallow() {
	raw := "" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NoError(err)

	plumbing.HashesSort(su.Unshallows)

	s.Len(su.Shallows, 0)
	s.Len(su.Unshallows, 2)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}, su.Unshallows)
}

func (s *ShallowUpdateSuite) TestDecodeMalformed() {
	raw := "" +
		"0035unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0000"

	su := &ShallowUpdate{}
	err := su.Decode(bytes.NewBufferString(raw))
	s.NotNil(err)
}

func (s *ShallowUpdateSuite) TestDecodeNotHex() {
	for _, raw := range []string{
		"0034shallow " + strings.Repeat("z", 40) + "0000",
		"0036unshallow " + strings.Repeat("z", 40) + "0000",
		"004cshallow " + strings.Repeat("z", 64) + "0000",
		"004eunshallow " + strings.Repeat("z", 64) + "0000",
	} {
		su := &ShallowUpdate{}
		s.Error(su.Decode(bytes.NewBufferString(raw)), raw)
	}
}

func (s *ShallowUpdateSuite) TestEncodeEmpty() {
	su := &ShallowUpdate{}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))
	s.Equal("0000", buf.String())
}

func (s *ShallowUpdateSuite) TestEncode() {
	su := &ShallowUpdate{
		Shallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
		Unshallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))

	expected := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	s.Equal(expected, buf.String())
}

func (s *ShallowUpdateSuite) TestEncodeShallow() {
	su := &ShallowUpdate{
		Shallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))

	expected := "" +
		"0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0034shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	s.Equal(expected, buf.String())
}

func (s *ShallowUpdateSuite) TestEncodeUnshallow() {
	su := &ShallowUpdate{
		Unshallows: []plumbing.Hash{
			plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	buf := bytes.NewBuffer(nil)
	s.Nil(su.Encode(buf))

	expected := "" +
		"0036unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"0036unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
		"0000"

	s.Equal(expected, buf.String())
}

func (s *ShallowUpdateSuite) TestDecodeSHA256() {
	shallow := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unshallow := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	raw := "" +
		"004cshallow " + shallow +
		"004funshallow " + unshallow + "\n" +
		"0000"

	su := &ShallowUpdate{}
	s.Require().NoError(su.Decode(bytes.NewBufferString(raw)))

	s.Equal([]plumbing.Hash{plumbing.NewHash(shallow)}, su.Shallows)
	s.Equal([]plumbing.Hash{plumbing.NewHash(unshallow)}, su.Unshallows)
}
