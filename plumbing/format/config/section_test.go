package config

import (
	. "gopkg.in/check.v1"
)

type SectionSuite struct{}

var _ = Suite(&SectionSuite{})

func (s *SectionSuite) TestSections_GoString(c *C) {
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
	c.Assert(sects.GoString(), Equals, expected)
}

func (s *SectionSuite) TestSubsections_GoString(c *C) {
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
	c.Assert(sects.GoString(), Equals, expected)
}

func (s *SectionSuite) TestSection_IsName(c *C) {
	sect := &Section{
		Name: "name1",
	}

	c.Assert(sect.IsName("name1"), Equals, true)
	c.Assert(sect.IsName("Name1"), Equals, true)
}

func (s *SectionSuite) TestSection_Subsection(c *C) {
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

	c.Assert(sect.Subsection("name1"), DeepEquals, subSect1)

	subSect2 := &Subsection{
		Name: "name2",
	}
	c.Assert(sect.Subsection("name2"), DeepEquals, subSect2)
}

func (s *SectionSuite) TestSection_HasSubsection(c *C) {
	sect := &Section{
		Subsections: Subsections{
			&Subsection{
				Name: "name1",
			},
		},
	}

	c.Assert(sect.HasSubsection("name1"), Equals, true)
	c.Assert(sect.HasSubsection("name2"), Equals, false)
}

func (s *SectionSuite) TestSection_RemoveSubsection(c *C) {
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
	c.Assert(sect.RemoveSubsection("name1"), DeepEquals, expected)
	c.Assert(sect.HasSubsection("name1"), Equals, false)
	c.Assert(sect.HasSubsection("name2"), Equals, true)
}

func (s *SectionSuite) TestSection_Option(c *C) {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.Option("otherkey"), Equals, "")
	c.Assert(sect.Option("key2"), Equals, "value2")
	c.Assert(sect.Option("key1"), Equals, "value3")
}

func (s *SectionSuite) TestSection_OptionAll(c *C) {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.OptionAll("otherkey"), DeepEquals, []string{})
	c.Assert(sect.OptionAll("key2"), DeepEquals, []string{"value2"})
	c.Assert(sect.OptionAll("key1"), DeepEquals, []string{"value1", "value3"})
}

func (s *SectionSuite) TestSection_HasOption(c *C) {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.HasOption("otherkey"), Equals, false)
	c.Assert(sect.HasOption("key2"), Equals, true)
	c.Assert(sect.HasOption("key1"), Equals, true)
}

func (s *SectionSuite) TestSection_AddOption(c *C) {
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
	c.Assert(sect.AddOption("key2", "value2"), DeepEquals, sect1)

	sect2 := &Section{
		Options: []*Option{
			{"key1", "value1"},
			{"key2", "value2"},
			{"key1", "value3"},
		},
	}
	c.Assert(sect.AddOption("key1", "value3"), DeepEquals, sect2)
}

func (s *SectionSuite) TestSection_SetOption(c *C) {
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
	c.Assert(sect.SetOption("key1", "value4"), DeepEquals, expected)
}

func (s *SectionSuite) TestSection_RemoveOption(c *C) {
	sect := &Section{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.RemoveOption("otherkey"), DeepEquals, sect)

	expected := &Section{
		Options: []*Option{
			{Key: "key2", Value: "value2"},
		},
	}
	c.Assert(sect.RemoveOption("key1"), DeepEquals, expected)
}

func (s *SectionSuite) TestSubsection_IsName(c *C) {
	sect := &Subsection{
		Name: "name1",
	}

	c.Assert(sect.IsName("name1"), Equals, true)
	c.Assert(sect.IsName("Name1"), Equals, false)
}

func (s *SectionSuite) TestSubsection_Option(c *C) {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.Option("otherkey"), Equals, "")
	c.Assert(sect.Option("key2"), Equals, "value2")
	c.Assert(sect.Option("key1"), Equals, "value3")
}

func (s *SectionSuite) TestSubsection_OptionAll(c *C) {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.OptionAll("otherkey"), DeepEquals, []string{})
	c.Assert(sect.OptionAll("key2"), DeepEquals, []string{"value2"})
	c.Assert(sect.OptionAll("key1"), DeepEquals, []string{"value1", "value3"})
}

func (s *SectionSuite) TestSubsection_HasOption(c *C) {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.HasOption("otherkey"), Equals, false)
	c.Assert(sect.HasOption("key2"), Equals, true)
	c.Assert(sect.HasOption("key1"), Equals, true)
}

func (s *SectionSuite) TestSubsection_AddOption(c *C) {
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
	c.Assert(sect.AddOption("key2", "value2"), DeepEquals, sect1)

	sect2 := &Subsection{
		Options: []*Option{
			{"key1", "value1"},
			{"key2", "value2"},
			{"key1", "value3"},
		},
	}
	c.Assert(sect.AddOption("key1", "value3"), DeepEquals, sect2)
}

func (s *SectionSuite) TestSubsection_SetOption(c *C) {
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
	c.Assert(sect.SetOption("key1", "value1", "value4"), DeepEquals, expected)
}

func (s *SectionSuite) TestSubsection_RemoveOption(c *C) {
	sect := &Subsection{
		Options: []*Option{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value3"},
		},
	}
	c.Assert(sect.RemoveOption("otherkey"), DeepEquals, sect)

	expected := &Subsection{
		Options: []*Option{
			{Key: "key2", Value: "value2"},
		},
	}
	c.Assert(sect.RemoveOption("key1"), DeepEquals, expected)
}
