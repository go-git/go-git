package hash

import "errors"

// ObjectFormat defines the object format, which is the hash algorithm
// a repository names its objects with. It is the value of the
// extensions.objectFormat configuration key.
type ObjectFormat string

const (
	// UnsetObjectFormat indicates no object format has been set. It is
	// handled as [DefaultObjectFormat].
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

// Size returns the hash size of the ObjectFormat. Unknown formats
// return the size of [DefaultObjectFormat].
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

// HexSize returns the hash size in hexadecimal format of the
// ObjectFormat. Unknown formats return the size of
// [DefaultObjectFormat].
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
