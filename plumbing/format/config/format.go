package config

import "errors"

// RepositoryFormatVersion represents the repository format version,
// as per defined at:
//
//	https://git-scm.com/docs/repository-version
type RepositoryFormatVersion string

const (
	// Version0 is the format defined by the initial version of git,
	// including but not limited to the format of the repository
	// directory, the repository configuration file, and the object
	// and ref storage.
	//
	// Specifying the complete behavior of git is beyond the scope
	// of this document.
	Version0 = "0"

	// Version1 is identical to version 0, with the following exceptions:
	//
	//   1. When reading the core.repositoryformatversion variable, a git
	//		implementation which supports version 1 MUST also read any
	//		configuration keys found in the extensions section of the
	//		configuration file.
	//
	//	 2. If a version-1 repository specifies any extensions.* keys that
	//		the running git has not implemented, the operation MUST NOT proceed.
	//		Similarly, if the value of any known key is not understood by the
	//		implementation, the operation MUST NOT proceed.
	//
	// Note that if no extensions are specified in the config file, then
	// core.repositoryformatversion SHOULD be set to 0 (setting it to 1 provides
	// no benefit, and makes the repository incompatible with older
	// implementations of git).
	Version1 = "1"

	// DefaultRepositoryFormatVersion holds the default repository format version.
	DefaultRepositoryFormatVersion = Version0
)

// ObjectFormat defines the object format.
type ObjectFormat string

const (
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
