package protocol

import (
	"errors"
	"fmt"
)

var ErrUnknownProtocol = errors.New("unknown Git Wire protocol")

// Version sets the preferred version for the Git wire protocol.
type Version int

const (
	Unknown Version = -1
	// V0 represents the original Wire protocol.
	V0 Version = iota
	// V1 represents the version V1 of the Wire protocol.
	V1
	// V2 represents the version V2 of the Wire protocol.
	V2
)

// String converts a Version into string.
// The Unknown version is converted to empty string.
func (v Version) String() string {
	switch v {
	case V0:
		return "0"
	case V1:
		return "1"
	case V2:
		return "2"
	}

	return ""
}

// Parse parses a string and returns the matching protocol version.
// Unrecognised strings will return a ErrUnknownProtocol.
func Parse(v string) (Version, error) {
	switch v {
	case "0":
		return V0, nil
	case "1":
		return V1, nil
	case "2":
		return V2, nil
	}

	return Unknown, fmt.Errorf("cannot parse %q: %w", v, ErrUnknownProtocol)
}
