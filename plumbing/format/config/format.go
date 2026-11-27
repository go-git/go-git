package config

import "errors"

// RepositoryFormatVersion represents the repository format version,
// as per defined at:
//
//	https://git-scm.com/docs/repository-version
type RepositoryFormatVersion string

const (
	// Version0 is the original repository format. Extensions are not part
	// of it.
	Version0 = "0"

	// Version1 is Version0 with support for extensions: the keys of the
	// extensions section of the configuration file take effect, and a
	// repository that sets one the implementation does not know, or sets
	// a known one to a value it does not understand, must be refused.
	//
	// A repository that uses no extensions gains nothing from this version
	// and is better left at Version0, which older implementations can read.
	Version1 = "1"

	// DefaultRepositoryFormatVersion holds the default repository format version.
	DefaultRepositoryFormatVersion = Version0
)

// ObjectFormat defines the object format.
type ObjectFormat string

const (
	// UnsetObjectFormat indicates no object format has been set.
	UnsetObjectFormat ObjectFormat = ""

	// SHA1 represents the object format used for SHA1.
	SHA1 ObjectFormat = "sha1"

	// SHA256 represents the object format used for SHA256.
	SHA256 ObjectFormat = "sha256"

	// DefaultObjectFormat holds the default object format.
	DefaultObjectFormat = SHA1
)

// String returns the string representation of the ObjectFormat.
func (f ObjectFormat) String() string {
	return string(f)
}

// Size returns the hash size of the ObjectFormat.
func (f ObjectFormat) Size() int {
	switch f {
	case SHA1:
		return SHA1Size
	case SHA256:
		return SHA256Size
	default:
		return DefaultObjectFormat.Size()
	}
}

// HexSize returns the hash size in hexadecimal format of the ObjectFormat.
func (f ObjectFormat) HexSize() int {
	switch f {
	case SHA1:
		return SHA1HexSize
	case SHA256:
		return SHA256HexSize
	default:
		return DefaultObjectFormat.HexSize()
	}
}

// ErrInvalidObjectFormat is returned when an invalid ObjectFormat is used.
var ErrInvalidObjectFormat = errors.New("invalid object format")

// ErrObjectFormatMismatch is returned when data or an object ID uses another
// object format than the one expected, such as a SHA-256 hash given to a
// SHA-1 index.
var ErrObjectFormatMismatch = errors.New("object format mismatch")

const (
	// SHA1Size is the size of SHA1 hash.
	SHA1Size = 20
	// SHA256Size is the size of SHA256 hash.
	SHA256Size = 32
	// SHA1HexSize is the size of SHA1 hash in hexadecimal format.
	SHA1HexSize = SHA1Size * 2
	// SHA256HexSize is the size of SHA256 hash in hexadecimal format.
	SHA256HexSize = SHA256Size * 2
)
