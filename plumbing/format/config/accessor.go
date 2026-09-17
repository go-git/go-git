package config

// This file adds the canonical git "config set" access API on top of the
// Section/Subsection/Option model: typed getters and setters keyed by a flat
// "section[.subsection].variable" string, mirroring git's
// git_configset_get_value, git_configset_get_bool, git_configset_get_int and
// friends. Reads merge all blocks of a repeated section, with the last value
// winning, exactly as git does.

// allOptions collects, in file order, the options of every block of the named
// section (and subsection, when given). git treats repeated [section] blocks as
// a single logical section, so accessors must look across all of them.
func (c *Config) allOptions(section, subsection string) Options {
	var opts Options
	for _, s := range c.Sections {
		if !s.IsName(section) {
			continue
		}
		if subsection == "" {
			opts = append(opts, s.Options...)
			continue
		}
		for _, ss := range s.Subsections {
			if ss.IsName(subsection) {
				opts = append(opts, ss.Options...)
			}
		}
	}
	return opts
}

// Lookup returns the last value set for the canonical key and whether it was
// present at all. It is the equivalent of git's git_configset_get_value.
func (c *Config) Lookup(key string) (string, bool) {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Lookup(variable)
}

// Get returns the last value set for the canonical key, or the empty string if
// the key is absent. Use Lookup to distinguish an absent key from an empty
// value.
func (c *Config) Get(key string) string {
	v, _ := c.Lookup(key)
	return v
}

// GetAll returns every value set for the canonical key, in file order. It is
// the equivalent of git's git_configset_get_value_multi.
func (c *Config) GetAll(key string) []string {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).GetAll(variable)
}

// Has reports whether the canonical key is present, regardless of its value.
func (c *Config) Has(key string) bool {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Has(variable)
}

// String returns the string value of the canonical key, or def when the key is
// absent.
func (c *Config) String(key, def string) string {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).String(variable, def)
}

// Bool returns the boolean value of the canonical key, or def when the key is
// absent. A present but unparseable value returns def together with
// ErrInvalidBool.
func (c *Config) Bool(key string, def bool) (bool, error) {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Bool(variable, def)
}

// Int returns the integer value of the canonical key, or def when the key is
// absent. A present but unparseable value returns def together with an error.
func (c *Config) Int(key string, def int) (int, error) {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Int(variable, def)
}

// Int64 returns the int64 value of the canonical key, or def when the key is
// absent. A present but unparseable value returns def together with an error.
func (c *Config) Int64(key string, def int64) (int64, error) {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Int64(variable, def)
}

// Uint returns the unsigned value of the canonical key, or def when the key is
// absent. A present but unparseable value returns def together with an error.
func (c *Config) Uint(key string, def uint) (uint, error) {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Uint(variable, def)
}

// Uint64 returns the unsigned 64-bit value of the canonical key, or def when
// the key is absent. A present but unparseable value returns def together with
// an error.
func (c *Config) Uint64(key string, def uint64) (uint64, error) {
	section, subsection, variable := SplitKey(key)
	return c.allOptions(section, subsection).Uint64(variable, def)
}

// Set sets the canonical key to value, replacing any existing values
// (last-wins). It returns ErrInvalidKey if key is not a valid config key.
func (c *Config) Set(key, value string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	section, subsection, variable := SplitKey(key)
	c.SetOption(section, subsection, variable, value)
	return nil
}

// Add appends value to the canonical key without removing existing values,
// producing a multi-valued key. It returns ErrInvalidKey if key is not a valid
// config key.
func (c *Config) Add(key, value string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	section, subsection, variable := SplitKey(key)
	c.AddOption(section, subsection, variable, value)
	return nil
}

// Unset removes every value of the canonical key from the section block that
// holds it. It reports whether anything was removed.
func (c *Config) Unset(key string) bool {
	section, subsection, variable := SplitKey(key)
	if !c.allOptions(section, subsection).Has(variable) {
		return false
	}
	if subsection == "" {
		c.Section(section).RemoveOption(variable)
	} else {
		c.Section(section).Subsection(subsection).RemoveOption(variable)
	}
	return true
}
