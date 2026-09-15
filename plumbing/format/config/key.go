package config

import (
	"errors"
	"strings"
)

// ErrInvalidKey is returned when a config key is not a valid
// "section[.subsection].variable" key as understood by git.
var ErrInvalidKey = errors.New("config: invalid key")

// SplitKey splits a canonical git config key into its components. Following
// git's parsing rules, the section is the text before the first dot, the
// variable name is the text after the last dot, and any text in between is the
// subsection (which may itself contain dots, e.g. a URL). Keys without a dot
// yield an empty variable, and keys with a single dot have no subsection.
//
// SplitKey performs no validation; use IsValidKey or ValidateKey for that.
//
//	"core.bare"                     -> "core", "",              "bare"
//	"remote.origin.url"             -> "remote", "origin",      "url"
//	"url.git@github.com:.insteadOf" -> "url", "git@github.com:", "insteadOf"
func SplitKey(key string) (section, subsection, variable string) {
	first := strings.IndexByte(key, '.')
	if first < 0 {
		return key, "", ""
	}

	last := strings.LastIndexByte(key, '.')
	section = key[:first]
	variable = key[last+1:]
	if last > first {
		subsection = key[first+1 : last]
	}
	return section, subsection, variable
}

// IsValidKey reports whether key is a valid git config key, equivalent to git's
// git_config_key_is_valid.
func IsValidKey(key string) bool {
	return ValidateKey(key) == nil
}

// ValidateKey checks that key is a valid "section[.subsection].variable" key
// and returns ErrInvalidKey otherwise. It mirrors git's do_parse_config_key:
// the section and variable names may contain only alphanumeric characters and
// '-', the variable name must start with a letter, and the subsection (if any)
// may contain any character except a newline.
//
// Reference: https://github.com/git/git/blob/master/config.c (do_parse_config_key)
func ValidateKey(key string) error {
	last := strings.LastIndexByte(key, '.')
	if last <= 0 || last == len(key)-1 {
		return ErrInvalidKey
	}

	first := strings.IndexByte(key, '.')
	section := key[:first]
	subsection := ""
	if last > first {
		subsection = key[first+1 : last]
	}
	variable := key[last+1:]

	if !validName(section) || !validVariable(variable) {
		return ErrInvalidKey
	}
	if strings.ContainsRune(subsection, '\n') {
		return ErrInvalidKey
	}
	return nil
}

// isKeyChar reports whether c is allowed in a section or variable name,
// matching git's iskeychar (ASCII alphanumeric or '-').
func isKeyChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-'
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// validName reports whether name is a valid section or non-leading variable
// component: non-empty and composed only of key characters.
func validName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isKeyChar(name[i]) {
			return false
		}
	}
	return true
}

// validVariable reports whether name is a valid variable name: it must be a
// valid name and start with a letter.
func validVariable(name string) bool {
	return validName(name) && isAlpha(name[0])
}
