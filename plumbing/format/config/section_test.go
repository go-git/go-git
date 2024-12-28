package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type SectionSuite struct {
	suite.Suite
}

func TestSectionSuite(t *testing.T) {
	suite.Run(t, new(SectionSuite))
}

func (s *SectionSuite) TestSections_GoString() {
	sects := Sections{
		&Section{
			Options: []*Option{
				{Key: "key1", Value: "value1"},
				{Key: "key2", Value: "value2"},
			},
		},
		&Section{
			Options: []*Option{
				{Key: "key1", Value: "value3"},
				{Key: "key2", Value: "value4"},
			},
		},
	}

	expected := "&config.Section{Name:\"\", Options:&config.Option{Key:\"key1\", Value:\"value1\"}, &config.Option{Key:\"key2\", Value:\"value2\"}, Subsections:}, &config.Section{Name:\"\", Options:&config.Option{Key:\"key1\", Value:\"value3\"}, &config.Option{Key:\"key2\", Value:\"value4\"}, Subsections:}"
	s.Equal(expected, sects.GoString())
}

func (s *SectionSuite) TestSubsections_GoString() {
	sects := Subsections{
		&Subsection{
			Options: []*Option{
				{Key: "key1", Value: "value1"},
				{Key: "key2", Value: "value2"},
				{Key: "key1", Value: "value3"},
			},
		},
		&Subsection{
			Options: []*Option{
				{Key: "key1", Value: "value1"},
				{Key: "key2", Value: "value2"},
				{Key: "key1", Value: "value3"},
			},
		},
	}

	expected := "&config.Subsection{Name:\"\", Options:&config.Option{Key:\"key1\", Value:\"value1\"}, &config.Option{Key:\"key2\", Value:\"value2\"}, &config.Option{Key:\"key1\", Value:\"value3\"}}, &config.Subsection{Name:\"\", Options:&config.Option{Key:\"key1\", Value:\"value1\"}, &config.Option{Key:\"key2\", Value:\"value2\"}, &config.Option{Key:\"key1\", Value:\"value3\"}}"
	s.Equal(expected, sects.GoString())
}

func (s *SectionSuite) TestSection_IsName() {
	sect := &Section{
		Name: "name1",
	}

	s.True(sect.IsName("name1"))
	s.True(sect.IsName("Name1"))
}

func (s *SectionSuite) TestSection_Subsection() {
	subSect1 := &Subsection{
		Name: "name1",
		Options: Options{
			&Option{Key: "key1", Value: "value1"},
		},
	}
	sect := &Section{
		Subsections: Subsections{
			subSect1,
		},
	}

	s.Equal(subSect1, sect.Subsection("name1"))

	subSect2 := &Subsection{
		Name: "name2",
	}
	s.Equal(subSect2, sect.Subsection("name2"))
}

func (s *SectionSuite) TestSection_HasSubsection() {
	sect := &Section{
		Subsections: Subsections{
			&Subsection{
				Name: "name1",
			},
		},
	}

	s.True(sect.HasSubsection("name1"))
	s.False(sect.HasSubsection("name2"))
}

func (s *SectionSuite) TestSection_RemoveSubsection() {
	sect := &Section{
		Subsections: Subsections{
			&Subsection{
				Name: "name1",
			},
			&Subsection{
				Name: "name2",
			},
		},
	}

	expected := &Section{
		Subsections: Subsections{
			&Subsection{
				Name: "name2",
			},
		},
	}
	s.Equal(expected, sect.RemoveSubsection("name1"))
	s.False(sect.HasSubsection("name1"))
	s.True(sect.HasSubsection("name2"))
}

func (s *SectionSuite) TestSection_Option() {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.Equal("", sect.Option("otherkey"))
	s.Equal("value2", sect.Option("key2"))
	s.Equal("value3", sect.Option("key1"))
}

func (s *SectionSuite) TestSection_OptionAll() {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.Equal([]string{}, sect.OptionAll("otherkey"))
	s.Equal([]string{"value2"}, sect.OptionAll("key2"))
	s.Equal([]string{"value1", "value3"}, sect.OptionAll("key1"))
}

func (s *SectionSuite) TestSection_HasOption() {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.False(sect.HasOption("otherkey"))
	s.True(sect.HasOption("key2"))
	s.True(sect.HasOption("key1"))
}

func (s *SectionSuite) TestSection_AddOption() {
	sect := &Section{
		Options: []*Option{
			{"key1", "value1"},
		},
	}
	sect1 := &Section{
		Options: []*Option{
			{"key1", "value1"},
			{"key2", "value2"},
		},
	}
	s.Equal(sect1, sect.AddOption("key2", "value2"))

	sect2 := &Section{
		Options: []*Option{
			{"key1", "value1"},
			{"key2", "value2"},
			{"key1", "value3"},
		},
	}
	s.Equal(sect2, sect.AddOption("key1", "value3"))
}

func (s *SectionSuite) TestSection_SetOption() {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
		},
	}

	expected := &Section{
		Options: []*Option{
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value4"},
		},
	}
	s.Equal(expected, sect.SetOption("key1", "value4"))
}

func (s *SectionSuite) TestSection_RemoveOption() {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.Equal(sect, sect.RemoveOption("otherkey"))

	expected := &Section{
		Options: []*Option{
			{Key: "key2", Value: "value2"},
		},
	}
	s.Equal(expected, sect.RemoveOption("key1"))
}

func (s *SectionSuite) TestSubsection_IsName() {
	sect := &Subsection{
		Name: "name1",
	}

	s.True(sect.IsName("name1"))
	s.False(sect.IsName("Name1"))
}

func (s *SectionSuite) TestSubsection_Option() {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.Equal("", sect.Option("otherkey"))
	s.Equal("value2", sect.Option("key2"))
	s.Equal("value3", sect.Option("key1"))
}

func (s *SectionSuite) TestSubsection_OptionAll() {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.Equal([]string{}, sect.OptionAll("otherkey"))
	s.Equal([]string{"value2"}, sect.OptionAll("key2"))
	s.Equal([]string{"value1", "value3"}, sect.OptionAll("key1"))
}

func (s *SectionSuite) TestSubsection_HasOption() {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.False(sect.HasOption("otherkey"))
	s.True(sect.HasOption("key2"))
	s.True(sect.HasOption("key1"))
}

func (s *SectionSuite) TestSubsection_AddOption() {
	sect := &Subsection{
		Options: []*Option{
			{"key1", "value1"},
		},
	}
	sect1 := &Subsection{
		Options: []*Option{
			{"key1", "value1"},
			{"key2", "value2"},
		},
	}
	s.Equal(sect1, sect.AddOption("key2", "value2"))

	sect2 := &Subsection{
		Options: []*Option{
			{"key1", "value1"},
			{"key2", "value2"},
			{"key1", "value3"},
		},
	}
	s.Equal(sect2, sect.AddOption("key1", "value3"))
}

func (s *SectionSuite) TestSubsection_SetOption() {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}

	expected := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value4"},
		},
	}
	s.Equal(expected, sect.SetOption("key1", "value1", "value4"))
}

func (s *SectionSuite) TestSubsection_RemoveOption() {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	s.Equal(sect, sect.RemoveOption("otherkey"))

	expected := &Subsection{
		Options: []*Option{
			{Key: "key2", Value: "value2"},
		},
	}
	s.Equal(expected, sect.RemoveOption("key1"))
}
