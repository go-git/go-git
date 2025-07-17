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

func (s *CommonSuite) TestConfig_GetOption() {
	var cfg *Config
	var obtained string

	// when option does not exist, return empty string
	cfg = New()
	obtained = cfg.GetOption("section", NoSubsection, "key1")
	s.Equal("", obtained)

	// when single value exists, return value
	cfg = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Options: []*Option{
					{Key: "key1", Value: "value1"},
				},
			},
		},
	}
	obtained = cfg.GetOption("section", NoSubsection, "key1")
	s.Equal("value1", obtained)

	// when multiple values exist, return last value
	cfg = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Options: []*Option{
					{Key: "key1", Value: "value1"},
					{Key: "key1", Value: "value2"},
				},
			},
		},
	}
	obtained = cfg.GetOption("section", NoSubsection, "key1")
	s.Equal("value2", obtained)

	// when subsection option does not exist, return empty string
	cfg = New()
	obtained = cfg.GetOption("section", "subsection", "key1")
	s.Equal("", obtained)

	// when subsection single value exists, return value
	cfg = &Config{
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
	obtained = cfg.GetOption("section", "subsection", "key1")
	s.Equal("value1", obtained)

	// when multiple values exist, return last value
	cfg = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Subsections: []*Subsection{
					{
						Name: "subsection",
						Options: []*Option{
							{Key: "key1", Value: "value1"},
							{Key: "key1", Value: "value2"},
						},
					},
				},
			},
		},
	}
	obtained = cfg.GetOption("section", "subsection", "key1")
	s.Equal("value2", obtained)
}

func (s *CommonSuite) TestConfig_GetAllOptions() {
	var cfg *Config
	var obtained []string

	// when option does not exist, return empty string
	cfg = New()
	obtained = cfg.GetAllOptions("section", NoSubsection, "key1")
	s.Equal([]string{}, obtained)

	// when single value exists, return value
	cfg = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Options: []*Option{
					{Key: "key1", Value: "value1"},
				},
			},
		},
	}
	obtained = cfg.GetAllOptions("section", NoSubsection, "key1")
	s.Equal([]string{"value1"}, obtained)

	// when multiple values exist, return last value
	cfg = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Options: []*Option{
					{Key: "key1", Value: "value1"},
					{Key: "key1", Value: "value2"},
				},
			},
		},
	}
	obtained = cfg.GetAllOptions("section", NoSubsection, "key1")
	s.Equal([]string{"value1", "value2"}, obtained)

	// when subsection option does not exist, return empty string
	cfg = New()
	obtained = cfg.GetAllOptions("section", "subsection", "key1")
	s.Equal([]string{}, obtained)

	// when subsection single value exists, return value
	cfg = &Config{
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
	obtained = cfg.GetAllOptions("section", "subsection", "key1")
	s.Equal([]string{"value1"}, obtained)

	// when multiple values exist, return last value
	cfg = &Config{
		Sections: []*Section{
			{
				Name: "section",
				Subsections: []*Subsection{
					{
						Name: "subsection",
						Options: []*Option{
							{Key: "key1", Value: "value1"},
							{Key: "key1", Value: "value2"},
						},
					},
				},
			},
		},
	}
	obtained = cfg.GetAllOptions("section", "subsection", "key1")
	s.Equal([]string{"value1", "value2"}, obtained)
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
