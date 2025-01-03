package packp

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/suite"
)

type ShallowUpdateSuite struct {
	suite.Suite
}

func TestShallowUpdateSuite(t *testing.T) {
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
		"0035shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0035shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"0037unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0037unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
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
		"0035shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0035shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
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
		"0037unshallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0037unshallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"0000"

	s.Equal(expected, buf.String())
}
