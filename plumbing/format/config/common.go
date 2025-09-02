package config

// New creates a new config instance.
func New() *Config {
	return &Config{}
}

// Config contains all the sections, comments and includes from a config file.
type Config struct {
	Comment  *Comment
	Sections Sections
	Includes Includes
}

// Includes is a list of Includes in a config file.
type Includes []*Include

// Include is a reference to an included config file.
type Include struct {
	Path   string
	Config *Config
}

// Comment string without the prefix '#' or ';'.
type Comment string

const (
	// NoSubsection token is passed to Config.Section and Config.SetSection to
	// represent the absence of a section.
	NoSubsection = ""
)

// Section returns a existing section with the given name or creates a new one.
func (c *Config) Section(name string) *Section {
	for i := len(c.Sections) - 1; i >= 0; i-- {
		s := c.Sections[i]
		if s.IsName(name) {
			return s
		}
	}

	s := &Section{Name: name}
	c.Sections = append(c.Sections, s)
	return s
}

// HasSection checks if the Config has a section with the specified name.
func (c *Config) HasSection(name string) bool {
	for _, s := range c.Sections {
		if s.IsName(name) {
			return true
		}
	}
	return false
}

// RemoveSection removes a section from a config file.
func (c *Config) RemoveSection(name string) *Config {
	result := Sections{}
	for _, s := range c.Sections {
		if !s.IsName(name) {
			result = append(result, s)
		}
	}

	c.Sections = result
	return c
}

// RemoveSubsection remove a subsection from a config file.
func (c *Config) RemoveSubsection(section string, subsection string) *Config {
	for _, s := range c.Sections {
		if s.IsName(section) {
			result := Subsections{}
			for _, ss := range s.Subsections {
				if !ss.IsName(subsection) {
					result = append(result, ss)
				}
			}
			s.Subsections = result
		}
	}

	return c
}

// AddOption adds an option to a given section and subsection. Use the
// NoSubsection constant for the subsection argument if no subsection is wanted.
func (c *Config) AddOption(section string, subsection string, key string, value string) *Config {
	if subsection == NoSubsection {
		c.Section(section).AddOption(key, value)
	} else {
		c.Section(section).Subsection(subsection).AddOption(key, value)
	}

	return c
}

// SetOption sets an option to a given section and subsection. Use the
// NoSubsection constant for the subsection argument if no subsection is wanted.
func (c *Config) SetOption(section string, subsection string, key string, value ...string) *Config {
	if subsection == NoSubsection {
		c.Section(section).SetOption(key, value...)
	} else {
		c.Section(section).Subsection(subsection).SetOption(key, value...)
	}

	return c
}

// GetOption gets the value of a named Option from the Section and Subsection. Use the
// NoSubsection constant for the subsection argument if no subsection is wanted. If the
// option does not exist or is not set, it returns the empty string. Note that there
// is no difference. This matches git behaviour since git v1.8.1-rc1, if there are
// multiple definitions of a key, the last one wins.
func (c *Config) GetOption(section string, subsection string, key string) string {
	if subsection == NoSubsection {
		return c.Section(section).GetOption(key)
	} else {
		return c.Section(section).Subsection(subsection).GetOption(key)
	}
}

// GetAllOptions gets an option from a given section and subsection. Use the
// GetAllOptions gets all the values of a named Option from the Section. Use the
// NoSubsection constant for the subsection argument if no subsection is wanted.
// If the option does not exist or is not set, it returns an empty slice.
// This matches git behaviour since git v1.8.1-rc1.
func (c *Config) GetAllOptions(section string, subsection string, key string) []string {
	if subsection == NoSubsection {
		return c.Section(section).GetAllOptions(key)
	} else {
		return c.Section(section).Subsection(subsection).GetAllOptions(key)
	}
}
