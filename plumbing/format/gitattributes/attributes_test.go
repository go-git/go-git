package gitattributes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type AttributesSuite struct {
	suite.Suite
}

func TestAttributesSuite(t *testing.T) {
	suite.Run(t, new(AttributesSuite))
}

func (s *AttributesSuite) TestAttributes_ReadAttributes() {
	lines := []string{
		"[attr]sub -a",
		"[attr]add a",
		"* sub a",
		"* !a foo=bar -b c",
	}

	mas, err := ReadAttributes(strings.NewReader(strings.Join(lines, "\n")), nil, true)
	s.NoError(err)
	s.Len(mas, 4)

	s.Equal("sub", mas[0].Name)
	s.Nil(mas[0].Pattern)
	s.True(mas[0].Attributes[0].IsUnset())

	s.Equal("add", mas[1].Name)
	s.Nil(mas[1].Pattern)
	s.True(mas[1].Attributes[0].IsSet())

	s.Equal("*", mas[2].Name)
	s.NotNil(mas[2].Pattern)
	s.True(mas[2].Attributes[0].IsSet())

	s.Equal("*", mas[3].Name)
	s.NotNil(mas[3].Pattern)
	s.True(mas[3].Attributes[0].IsUnspecified())
	s.True(mas[3].Attributes[1].IsValueSet())
	s.Equal("bar", mas[3].Attributes[1].Value())
	s.True(mas[3].Attributes[2].IsUnset())
	s.True(mas[3].Attributes[3].IsSet())
	s.Equal("a: unspecified", mas[3].Attributes[0].String())
	s.Equal("foo: bar", mas[3].Attributes[1].String())
	s.Equal("b: unset", mas[3].Attributes[2].String())
	s.Equal("c: set", mas[3].Attributes[3].String())
}

func (s *AttributesSuite) TestAttributes_ReadAttributesDisallowMacro() {
	lines := []string{
		"[attr]sub -a",
		"* a add",
	}

	_, err := ReadAttributes(strings.NewReader(strings.Join(lines, "\n")), nil, false)
	s.ErrorIs(err, ErrMacroNotAllowed)
}

func (s *AttributesSuite) TestAttributes_ReadAttributesInvalidName() {
	lines := []string{
		"[attr]foo!bar -a",
	}

	_, err := ReadAttributes(strings.NewReader(strings.Join(lines, "\n")), nil, true)
	s.ErrorIs(err, ErrInvalidAttributeName)
}
