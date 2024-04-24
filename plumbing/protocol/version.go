package protocol

import "strconv"

// Version represents a Git protocol version.
type Version int

const (
	// VersionUnknown is an unknown protocol version.
	VersionUnknown Version = iota - 1

	// VersionV0 is the version 0 of the Git protocol.
	VersionV0

	// VersionV1 is the version 1 of the Git protocol.
	VersionV1

	// VersionV2 is the version 2 of the Git protocol.
	VersionV2
)

// String returns the string representation of the protocol version.
func (v Version) String() string {
	if v < 0 {
		return "unknown"
	}

	return "version " + strconv.Itoa(int(v))
}

// Parameter returns the string representation of the protocol version to be
// used in the Git wire protocol.
func (v Version) Parameter() string {
	if v < 0 {
		return ""
	}

	return "version=" + strconv.Itoa(int(v))
}
