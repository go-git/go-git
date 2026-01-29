package storer

import (
	"github.com/go-git/go-billy/v6"

	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Storer is a basic storer for encoded objects and references.
type Storer interface {
	EncodedObjectStorer
	ReferenceStorer
}

// Initializer should be implemented by storers that require to perform any
// operation when creating a new repository (i.e. git init).
type Initializer interface {
	// Init performs initialization of the storer and returns the error, if
	// any.
	Init() error
}

// FilesystemStorer is a storer that can be used to store objects and references
// in a filesystem. It is used by the filesystem storage.
type FilesystemStorer interface {
	Filesystem() billy.Filesystem
}

// ObjectFormatGetter expands a Storer so that it can support different Object Formats.
// Note that storage.Storer do not require this as they expose ConfigStorers, which is
// the source of truth for this information.
//
// Storers that don't implement this interface will default to the default Object Format.
type ObjectFormatGetter interface {
	// ObjectFormat returns the object format (hash algorithm) used by the Storer.
	ObjectFormat() formatcfg.ObjectFormat
}
