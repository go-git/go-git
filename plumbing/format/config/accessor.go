package config

import "strings"

// SplitKey splits a canonical Git config key into its components. Following
// git's parsing rules, the section is the text before the first dot, the
// variable name is the text after the last dot, and any text in between is the
// subsection (which may itself contain dots, e.g. a URL). Keys without a dot
// have an empty name and keys with a single dot have no subsection.
//
// Examples:
//
//	"core.bare"                       -> "core", "",                    "bare"
//	"remote.origin.url"               -> "remote", "origin",           "url"
//	"url.git@github.com:.insteadOf"   -> "url", "git@github.com:",      "insteadOf"
func SplitKey(key string) (section, subsection, name string) {
	first := strings.IndexByte(key, '.')
	if first < 0 {
		return key, "", ""
	}

	last := strings.LastIndexByte(key, '.')
	section = key[:first]
	name = key[last+1:]
	if last > first {
		subsection = key[first+1 : last]
	}
	return section, subsection, name
}

func (c *Config) options(section, subsection string) Options {
	for i := len(c.Sections) - 1; i >= 0; i-- {
		s := c.Sections[i]
		if !s.IsName(section) {
			continue
		}
		if subsection == "" {
			return s.Options
		}
		for j := len(s.Subsections) - 1; j >= 0; j-- {
			if s.Subsections[j].IsName(subsection) {
				return s.Subsections[j].Options
			}
		}
	}
	return nil
}

// Get returns the last value set for the canonical key (e.g. "remote.origin.url"),
// or the empty string if the key is not present. Last-value-wins matches git's
// behaviour since v1.8.1.
func (c *Config) Get(key string) string {
	section, subsection, name := SplitKey(key)
	return c.options(section, subsection).Get(name)
}

// GetAll returns every value set for the canonical key, in file order. It is the
// equivalent of git's git_config_get_value_multi.
func (c *Config) GetAll(key string) []string {
	section, subsection, name := SplitKey(key)
	return c.options(section, subsection).GetAll(name)
}

// Has reports whether the canonical key is present, regardless of its value.
func (c *Config) Has(key string) bool {
	section, subsection, name := SplitKey(key)
	return c.options(section, subsection).Has(name)
}

// Bool returns the boolean value of the canonical key, or def when the key is
// absent. The value is parsed with ParseBool; an unrecognised value also yields
// def.
func (c *Config) Bool(key string, def bool) bool {
	section, subsection, name := SplitKey(key)
	opts := c.options(section, subsection)
	if !opts.Has(name) {
		return def
	}
	v, err := ParseBool(opts.Get(name))
	if err != nil {
		return def
	}
	return v
}

// Int returns the integer value of the canonical key, or def when the key is
// absent. A present but unparseable value returns an error.
func (c *Config) Int(key string, def int) (int, error) {
	section, subsection, name := SplitKey(key)
	opts := c.options(section, subsection)
	if !opts.Has(name) {
		return def, nil
	}
	return ParseInt(opts.Get(name))
}

// Int64 returns the int64 value of the canonical key, or def when the key is
// absent. A present but unparseable value returns an error.
func (c *Config) Int64(key string, def int64) (int64, error) {
	section, subsection, name := SplitKey(key)
	opts := c.options(section, subsection)
	if !opts.Has(name) {
		return def, nil
	}
	return ParseInt64(opts.Get(name))
}

// Uint returns the unsigned value of the canonical key, or def when the key is
// absent. A present but unparseable value returns an error.
func (c *Config) Uint(key string, def uint64) (uint64, error) {
	section, subsection, name := SplitKey(key)
	opts := c.options(section, subsection)
	if !opts.Has(name) {
		return def, nil
	}
	return ParseUint(opts.Get(name))
}

// Set sets the canonical key to value, replacing any existing values
// (last-wins). It returns the Config to allow chaining.
func (c *Config) Set(key, value string) *Config {
	section, subsection, name := SplitKey(key)
	return c.SetOption(section, subsection, name, value)
}

// Add appends value to the canonical key without removing existing values,
// producing a multi-valued key. It returns the Config to allow chaining.
func (c *Config) Add(key, value string) *Config {
	section, subsection, name := SplitKey(key)
	return c.AddOption(section, subsection, name, value)
}

// Unset removes every value of the canonical key. It returns the Config to
// allow chaining.
func (c *Config) Unset(key string) *Config {
	section, subsection, name := SplitKey(key)
	if subsection == "" {
		c.Section(section).RemoveOption(name)
	} else {
		c.Section(section).Subsection(subsection).RemoveOption(name)
	}
	return c
}
