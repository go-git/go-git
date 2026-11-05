// Package gitattributes provides utilities for parsing and matching gitattributes files.
package gitattributes

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

const (
	commentPrefix = "#"
	eol           = "\n"
	macroPrefix   = "[attr]"
)

var (
	// ErrMacroNotAllowed is returned when a macro is used where it is not allowed.
	ErrMacroNotAllowed = errors.New("macro not allowed")
	// ErrInvalidAttributeName is returned when an invalid attribute name is used.
	ErrInvalidAttributeName = errors.New("invalid attribute name")
)

// MatchAttribute represents a gitattribute pattern match with its attributes.
type MatchAttribute struct {
	Name       string
	Pattern    Pattern
	Attributes []Attribute
}

type attributeState byte

const (
	attributeUnknown     attributeState = 0
	attributeSet         attributeState = 1
	attributeUnspecified attributeState = '!'
	attributeUnset       attributeState = '-'
	attributeSetValue    attributeState = '='
)

// Attribute represents a gitattribute.
type Attribute interface {
	Name() string
	IsSet() bool
	IsUnset() bool
	IsUnspecified() bool
	IsValueSet() bool
	Value() string
	String() string
}

type attribute struct {
	name  string
	state attributeState
	value string
}

func (a attribute) Name() string {
	return a.name
}

func (a attribute) IsSet() bool {
	return a.state == attributeSet
}

func (a attribute) IsUnset() bool {
	return a.state == attributeUnset
}

func (a attribute) IsUnspecified() bool {
	return a.state == attributeUnspecified
}

func (a attribute) IsValueSet() bool {
	return a.state == attributeSetValue
}

func (a attribute) Value() string {
	return a.value
}

func (a attribute) String() string {
	switch a.state {
	case attributeSet:
		return a.name + ": set"
	case attributeUnset:
		return a.name + ": unset"
	case attributeUnspecified:
		return a.name + ": unspecified"
	default:
		return a.name + ": " + a.value
	}
}

// ReadAttributes reads patterns and attributes from the gitattributes format.
func ReadAttributes(r io.Reader, domain []string, allowMacro bool) (attributes []MatchAttribute, err error) {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		attribute, err := ParseAttributesLine(scanner.Text(), domain, allowMacro)
		if err != nil {
			return attributes, err
		}
		if len(attribute.Name) == 0 {
			continue
		}

		attributes = append(attributes, attribute)
	}

	if err := scanner.Err(); err != nil {
		return attributes, err
	}

	return attributes, nil
}

// blank holds the characters Git separates a pattern and its attributes
// with. Other white space, such as U+00A0 or a vertical tab, is part of
// the pattern or the attribute.
const blank = " \t\r\n"

// ParseAttributesLine parses a gitattribute line, extracting path pattern and
// attributes.
//
// It follows Git's parse_attr_line: fields are separated by blanks only, a
// pattern in double quotes is C-unquoted, and a "-" or "!" prefix unsets or
// unspecifies an attribute and drops any value given to it.
//
// See https://github.com/git/git/blob/v2.54.0/attr.c#L276-L410.
func ParseAttributesLine(line string, domain []string, allowMacro bool) (m MatchAttribute, err error) {
	line = strings.TrimLeft(line, blank)
	if strings.HasPrefix(line, commentPrefix) || len(line) == 0 {
		return m, err
	}

	var name, states string
	if line[0] == '"' {
		var ok bool
		if name, states, ok = unquoteC(line); !ok {
			name, states = cutBlank(line)
		} else if name == "" {
			// A quoted empty pattern names no path.
			return MatchAttribute{}, nil
		}
	} else {
		name, states = cutBlank(line)
	}

	var macro bool
	macro, name, err = checkMacro(name, allowMacro)
	if err != nil {
		return m, err
	}

	m.Name = name
	for states = strings.TrimLeft(states, blank); states != ""; {
		var token string
		token, states = cutBlank(states)
		states = strings.TrimLeft(states, blank)

		attr, err := parseAttribute(token)
		if err != nil {
			return m, err
		}
		m.Attributes = append(m.Attributes, attr)
	}

	if !macro {
		m.Pattern = ParsePattern(name, domain)
	}
	return m, err
}

// cutBlank splits s at its first blank.
func cutBlank(s string) (field, rest string) {
	if i := strings.IndexAny(s, blank); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// parseAttribute parses one attribute of a line: name, -name, !name or
// name=value. As in Git, the value of a -name=value or !name=value is
// dropped, and the name is checked up to the "=".
func parseAttribute(token string) (attribute, error) {
	attr := attribute{state: attributeSet}
	switch attributeState(token[0]) {
	case attributeUnset, attributeUnspecified:
		attr.state = attributeState(token[0])
		token = token[1:]
	}

	name, value, hasValue := strings.Cut(token, "=")
	if !validAttributeName(name) {
		return attr, ErrInvalidAttributeName
	}
	attr.name = name
	if hasValue && attr.state == attributeSet {
		attr.state = attributeSetValue
		attr.value = value
	}
	return attr, nil
}

func checkMacro(name string, allowMacro bool) (macro bool, macroName string, err error) {
	if len(name) <= len(macroPrefix) || !strings.HasPrefix(name, macroPrefix) {
		return false, name, nil
	}
	if !allowMacro {
		return true, name, ErrMacroNotAllowed
	}

	macroName = name[len(macroPrefix):]
	if !validAttributeName(macroName) {
		return true, name, ErrInvalidAttributeName
	}
	return true, macroName, nil
}

func validAttributeName(name string) bool {
	if len(name) == 0 || name[0] == '-' {
		return false
	}

	for _, ch := range name {
		if (ch != '-' && ch != '.' && ch != '_') &&
			(ch < '0' || ch > '9') &&
			(ch < 'a' || ch > 'z') &&
			(ch < 'A' || ch > 'Z') {
			return false
		}
	}
	return true
}

// unquoteC C-unquotes the double-quoted string at the start of s, as Git's
// unquote_c_style does, and returns it with the rest of s. It reports
// false when s does not start with a well-formed quoted string, which Git
// then reads as an unquoted pattern.
func unquoteC(s string) (unquoted, rest string, ok bool) {
	if s == "" || s[0] != '"' {
		return "", s, false
	}

	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
			i++
			if i >= len(s) {
				return "", s, false
			}
			switch c = s[i]; c {
			case 'a':
				b.WriteByte('\a')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'v':
				b.WriteByte('\v')
			case '\\', '"':
				b.WriteByte(c)
			case '0', '1', '2', '3':
				if i+2 >= len(s) || !isOctal(s[i+1]) || !isOctal(s[i+2]) {
					return "", s, false
				}
				b.WriteByte((c-'0')<<6 | (s[i+1]-'0')<<3 | (s[i+2] - '0'))
				i += 2
			default:
				return "", s, false
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", s, false
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }
