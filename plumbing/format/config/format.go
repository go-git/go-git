package config

import "github.com/go-git/go-git/v6/plumbing/hash"

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

// ObjectFormat defines the object format. It is an alias of
// [hash.ObjectFormat], kept so that the format can be named from the
// configuration package.
type ObjectFormat = hash.ObjectFormat

const (
	// UnsetObjectFormat is [hash.UnsetObjectFormat].
	UnsetObjectFormat = hash.UnsetObjectFormat

	// SHA1 is [hash.SHA1].
	SHA1 = hash.SHA1

	// SHA256 is [hash.SHA256].
	SHA256 = hash.SHA256

	// DefaultObjectFormat is [hash.DefaultObjectFormat].
	DefaultObjectFormat = hash.DefaultObjectFormat
)

// ErrObjectFormatMismatch is [hash.ErrObjectFormatMismatch].
var ErrObjectFormatMismatch = hash.ErrObjectFormatMismatch

const (
	// SHA1Size is [hash.SHA1Size].
	SHA1Size = hash.SHA1Size
	// SHA256Size is [hash.SHA256Size].
	SHA256Size = hash.SHA256Size
	// SHA1HexSize is [hash.SHA1HexSize].
	SHA1HexSize = hash.SHA1HexSize
	// SHA256HexSize is [hash.SHA256HexSize].
	SHA256HexSize = hash.SHA256HexSize
)
