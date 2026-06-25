package config

import (
	"strconv"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// OptBool is a tri-state boolean: unset, explicitly false, or explicitly true.
// Its zero value (OptBoolUnset) means the setting was not specified, which
// allows merge logic based on reflect.Value.IsZero to skip unset fields while
// still letting an explicit "false" override a previously set "true".
type OptBool byte

const (
	// OptBoolUnset indicates the setting was not specified.
	OptBoolUnset OptBool = iota
	// OptBoolFalse indicates the setting was explicitly set to false.
	OptBoolFalse
	// OptBoolTrue indicates the setting was explicitly set to true.
	OptBoolTrue
)

// NewOptBool converts a plain bool into an OptBool.
func NewOptBool(v bool) OptBool {
	if v {
		return OptBoolTrue
	}
	return OptBoolFalse
}

// IsTrue returns whether the value is explicitly true.
func (o OptBool) IsTrue() bool { return o == OptBoolTrue }

// IsSet returns whether the value was explicitly specified (true or false).
func (o OptBool) IsSet() bool { return o != OptBoolUnset }

func (o OptBool) String() string {
	switch o {
	case OptBoolTrue:
		return "true"
	case OptBoolFalse:
		return "false"
	default:
		return "unset"
	}
}

// FormatBool returns the strconv-formatted value. Only meaningful when IsSet.
func (o OptBool) FormatBool() string {
	return strconv.FormatBool(o.IsTrue())
}

// parseConfigBool parses a Git boolean into an OptBool, preserving the
// tri-state distinction the struct API relies on. The boolean grammar is
// shared with the rest of go-git via format.ParseBool (true/yes/on and
// false/no/off case-insensitively, plus any integer). An empty value, which
// go-git uses to mean "key absent" here, and any unrecognised value leave the
// result unset so the caller's platform default applies.
//
// Reference: upstream Git git_parse_maybe_bool at parse.c.
func parseConfigBool(v string) OptBool {
	if v == "" {
		return OptBoolUnset
	}
	b, err := format.ParseBool(v)
	if err != nil {
		return OptBoolUnset
	}
	return NewOptBool(b)
}
