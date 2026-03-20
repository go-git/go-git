// Package protocol provides types for the Git wire protocol.
package protocol

import (
	"errors"
	"fmt"
)

// ErrUnknownProtocol is returned when an unknown protocol version is used.
var ErrUnknownProtocol = errors.New("unknown Git Wire protocol")

// Version sets the preferred version for the Git wire protocol.
type Version int

const (
	// V0 represents the original Wire protocol.
	V0 Version = iota
	// V1 represents the version V1 of the Wire protocol.
	V1
	// V2 represents the version V2 of the Wire protocol.
	V2

	// Undefined represents an undefined protocol version.
	Undefined Version = -1
)

// Versions is a bitmask of protocol versions.
type Versions uint8

const (
	// SupportV0 enables the original wire protocol.
	SupportV0 Versions = 1 << V0
	// SupportV1 enables version 1 of the wire protocol.
	SupportV1 Versions = 1 << V1
	// SupportV2 enables version 2 of the wire protocol.
	SupportV2 Versions = 1 << V2

	// SupportAll enables all protocol versions.
	SupportAll = SupportV0 | SupportV1 | SupportV2
)

// Has reports whether mask includes the given version.
func (m Versions) Has(v Version) bool {
	return m&(1<<v) != 0
}

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

	return Undefined, fmt.Errorf("cannot parse %q: %w", v, ErrUnknownProtocol)
}
