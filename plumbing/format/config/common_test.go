package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type CommonSuite struct {
	suite.Suite
}

func TestCommonSuite(t *testing.T) {
	suite.Run(t, new(CommonSuite))
}

func (s *CommonSuite) TestConfig_SetOption() {
	obtained := New().SetOption("section", NoSubsection, "key1", "value1")
	expected := &Config{
		Sections: []*Section{
			{
				Name: "section",
				Options: []*Option{
					{Key: "key1", Value: "value1"},
				},
			},
		},
	}
	s.Equal(expected, obtained)
	obtained = obtained.SetOption("section", NoSubsection, "key1", "value1")
	s.Equal(expected, obtained)

	obtained = New().SetOption("section", "subsection", "key1", "value1")
	expected = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Subsections: []*Subsection{
					{
						Name: "subsection",
						Options: []*Option{
							{Key: "key1", Value: "value1"},
						},
					},
				},
			},
		},
	}
	s.Equal(expected, obtained)
	obtained = obtained.SetOption("section", "subsection", "key1", "value1")
	s.Equal(expected, obtained)
}

func (s *CommonSuite) TestConfig_AddOption() {
	obtained := New().AddOption("section", NoSubsection, "key1", "value1")
	expected := &Config{
		Sections: []*Section{
			{
				Name: "section",
				Options: []*Option{
					{Key: "key1", Value: "value1"},
				},
			},
		},
	}
	s.Equal(expected, obtained)
}

func (s *CommonSuite) TestConfig_HasSection() {
	sect := New().
		AddOption("section1", "sub1", "key1", "value1").
		AddOption("section1", "sub2", "key1", "value1")
	s.True(sect.HasSection("section1"))
	s.False(sect.HasSection("section2"))
}

func (s *CommonSuite) TestConfig_RemoveSection() {
	sect := New().
		AddOption("section1", NoSubsection, "key1", "value1").
		AddOption("section2", NoSubsection, "key1", "value1")
	expected := New().
		AddOption("section1", NoSubsection, "key1", "value1")
	s.Equal(sect, sect.RemoveSection("other"))
	s.Equal(expected, sect.RemoveSection("section2"))
}

func (s *CommonSuite) TestConfig_RemoveSubsection() {
	sect := New().
		AddOption("section1", "sub1", "key1", "value1").
		AddOption("section1", "sub2", "key1", "value1")
	expected := New().
		AddOption("section1", "sub1", "key1", "value1")
	s.Equal(sect, sect.RemoveSubsection("section1", "other"))
	s.Equal(sect, sect.RemoveSubsection("other", "other"))
	s.Equal(expected, sect.RemoveSubsection("section1", "sub2"))
}
