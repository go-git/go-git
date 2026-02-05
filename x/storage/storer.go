package storage

import "github.com/go-git/go-git/v6/plumbing/format/config"

// ObjectFormatSetter is implemented by storage backends that support
// configuring the object format (hash algorithm) used for the repository.
//
// Storers that do not implement this interface will only be able to support
// the SHA1 ObjectFormat.
//
// This interface is experimental and may cease to exist in future go-git releases.
// For more info refer to #1832.
type ObjectFormatSetter interface {
	// SetObjectFormat configures the object format (hash algorithm) for this storage.
	SetObjectFormat(config.ObjectFormat) error
}

// ObjectFormatGetter expands a Storer so that it can support different Object Formats.
// Note that storage.Storer do not require this as they expose ConfigStorers, which is
// the source of truth for this information.
//
// Storers that don't implement this interface will default to the default Object Format.
type ObjectFormatGetter interface {
	// ObjectFormat returns the object format (hash algorithm) used by the Storer.
	ObjectFormat() config.ObjectFormat
}
