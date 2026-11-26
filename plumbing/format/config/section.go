package config

import (
	"fmt"
	"slices"
	"strings"
)

// Section is the representation of a section inside git configuration files.
// A section has a name, options of its own, and any number of named
// subsections, each with their own options:
//
//	[section]
//	    option1 = value1
//	[section "subsection"]
//	    option2 = value2
//	    option3
//
// An option written without a value, like option3, reads as boolean true.
// Section and option names are case-insensitive; subsection names are not.
type Section struct {
	Name        string
	Options     Options
	Subsections Subsections
}

// Subsection is a subsection of a Section.
type Subsection struct {
	Name    string
	Options Options
}

// Sections is a collection of Section.
type Sections []*Section

// GoString returns a Go-syntax representation of Sections.
func (s Sections) GoString() string {
	strs := make([]string, 0, len(s))
	for _, ss := range s {
		strs = append(strs, fmt.Sprintf("%#v", ss))
	}

	return strings.Join(strs, ", ")
}

// Subsections is a collection of Subsection.
type Subsections []*Subsection

// GoString returns a Go-syntax representation of Subsections.
func (s Subsections) GoString() string {
	strs := make([]string, 0, len(s))
	for _, ss := range s {
		strs = append(strs, fmt.Sprintf("%#v", ss))
	}

	return strings.Join(strs, ", ")
}

// IsName checks if the name provided is equals to the Section name, case insensitive.
func (s *Section) IsName(name string) bool {
	// NOTE: decodeIndex resolves section names by this same rule, using a
	// map. Keep the two in sync.
	return strings.EqualFold(s.Name, name)
}

// Subsection returns a Subsection from the specified Section. If the
// Subsection does not exists, new one is created and added to Section.
func (s *Section) Subsection(name string) *Subsection {
	for _, ss := range slices.Backward(s.Subsections) {
		if ss.IsName(name) {
			return ss
		}
	}

	return s.appendSubsection(name)
}

// appendSubsection adds a subsection with the given name and returns it. As
// with Config.appendSection, the caller is responsible for having established
// that no subsection of that name exists yet.
func (s *Section) appendSubsection(name string) *Subsection {
	ss := &Subsection{Name: name}
	s.Subsections = append(s.Subsections, ss)
	return ss
}

// HasSubsection checks if the Section has a Subsection with the specified name.
func (s *Section) HasSubsection(name string) bool {
	for _, ss := range s.Subsections {
		if ss.IsName(name) {
			return true
		}
	}

	return false
}

// RemoveSubsection removes a subsection from a Section.
func (s *Section) RemoveSubsection(name string) *Section {
	result := Subsections{}
	for _, s := range s.Subsections {
		if !s.IsName(name) {
			result = append(result, s)
		}
	}

	s.Subsections = result
	return s
}

// Option returns the value for the specified key. Empty string is returned if
// key does not exists.
func (s *Section) Option(key string) string {
	return s.Options.Get(key)
}

// OptionAll returns all possible values for an option with the specified key.
// If the option does not exists, an empty slice will be returned.
func (s *Section) OptionAll(key string) []string {
	return s.Options.GetAll(key)
}

// HasOption checks if the Section has an Option with the given key.
func (s *Section) HasOption(key string) bool {
	return s.Options.Has(key)
}

// AddOption adds a new Option to the Section. The updated Section is returned.
func (s *Section) AddOption(key, value string) *Section {
	s.Options = s.Options.withAddedOption(key, value)
	return s
}

// SetOption adds a new Option to the Section. If the option already exists, is replaced.
// The updated Section is returned.
func (s *Section) SetOption(key, value string) *Section {
	s.Options = s.Options.withSettedOption(key, value)
	return s
}

// RemoveOption removes an option with the specified key. The updated Section is returned.
func (s *Section) RemoveOption(key string) *Section {
	s.Options = s.Options.withoutOption(key)
	return s
}

// IsName checks if the name of the subsection is exactly the specified name.
func (s *Subsection) IsName(name string) bool {
	// NOTE: Unlike Section.IsName, this is case-sensitive, and decodeIndex
	// keys subsections verbatim to match. Keep the two in sync.
	return s.Name == name
}

// Option returns an option with the specified key. If the option does not exists,
// empty spring will be returned.
func (s *Subsection) Option(key string) string {
	return s.Options.Get(key)
}

// OptionAll returns all possible values for an option with the specified key.
// If the option does not exists, an empty slice will be returned.
func (s *Subsection) OptionAll(key string) []string {
	return s.Options.GetAll(key)
}

// HasOption checks if the Subsection has an Option with the given key.
func (s *Subsection) HasOption(key string) bool {
	return s.Options.Has(key)
}

// AddOption adds a new Option to the Subsection. The updated Subsection is returned.
func (s *Subsection) AddOption(key, value string) *Subsection {
	s.Options = s.Options.withAddedOption(key, value)
	return s
}

// SetOption adds a new Option to the Subsection. If the option already exists, is replaced.
// The updated Subsection is returned.
func (s *Subsection) SetOption(key string, value ...string) *Subsection {
	s.Options = s.Options.withSettedOption(key, value...)
	return s
}

// RemoveOption removes the option with the specified key. The updated Subsection is returned.
func (s *Subsection) RemoveOption(key string) *Subsection {
	s.Options = s.Options.withoutOption(key)
	return s
}
