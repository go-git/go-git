package packp

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"
)

type PushOptionsSuite struct {
	suite.Suite
}

func TestPushOptionsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PushOptionsSuite))
}

func (s *PushOptionsSuite) TestPushOptionsEncode() {
	var r PushOptions
	r.Options = []string{
		"SomeKey=SomeValue",
		"AnotherKey=AnotherValue",
	}

	expected := pktlines(s.T(),
		"SomeKey=SomeValue",
		"AnotherKey=AnotherValue",
		"",
	)

	var buf bytes.Buffer
	s.Require().Nil(r.Encode(&buf))
	s.Require().Equal(expected, buf.Bytes())
}

func (s *PushOptionsSuite) TestPushOptionsEncodeEmpty() {
	var r PushOptions
	r.Options = []string{}

	expected := pktlines(s.T(), "")

	var buf bytes.Buffer
	s.Require().Nil(r.Encode(&buf))
	s.Require().Equal(expected, buf.Bytes())
}

func (s *PushOptionsSuite) TestPushOptionsEncodeInvalidOption() {
	cases := []struct {
		name    string
		options []string
	}{
		{
			name:    "option with newline",
			options: []string{"SomeKey=SomeValue\n"},
		},
		{
			name:    "option with null byte",
			options: []string{"SomeKey=SomeValue\x00"},
		},
	}

	for _, c := range cases {
		s.Run(c.name, func() {
			var r PushOptions
			r.Options = c.options

			var buf bytes.Buffer
			err := r.Encode(&buf)
			s.Require().Error(err)
			s.Require().True(errors.Is(err, ErrInvalidPushOption))
		})
	}
}

func (s *PushOptionsSuite) TestPushOptionsDecode() {
	var r PushOptions
	r.Options = nil

	input := pktlines(s.T(),
		"SomeKey=SomeValue",
		"AnotherKey=AnotherValue",
		"",
	)

	s.Require().Nil(r.Decode(bytes.NewReader(input)))

	expected := []string{
		"SomeKey=SomeValue",
		"AnotherKey=AnotherValue",
	}

	s.Require().Equal(expected, r.Options)
}

func (s *PushOptionsSuite) TestPushOptionsDecodeEmpty() {
	var r PushOptions
	r.Options = nil

	input := pktlines(s.T(), "")

	s.Require().Nil(r.Decode(bytes.NewReader(input)))
	s.Require().Empty(r.Options)
}

func (s *PushOptionsSuite) TestPushOptionsDecodeInvalidOption() {
	cases := []struct {
		name  string
		input []byte
	}{
		{
			name:  "option with newline",
			input: pktlines(s.T(), "SomeKey=SomeValue\n", ""),
		},
		{
			name:  "option with null byte",
			input: pktlines(s.T(), "SomeKey=SomeValue\x00", ""),
		},
	}

	for _, c := range cases {
		s.Run(c.name, func() {
			var r PushOptions
			r.Options = nil

			err := r.Decode(bytes.NewReader(c.input))
			s.Require().Error(err)
			s.Require().True(errors.Is(err, ErrInvalidPushOption))
		})
	}
}
