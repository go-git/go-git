package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/suite"
)

type PushOptionsSuite struct {
	suite.Suite
}

func TestPushOptionsSuite(t *testing.T) {
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
