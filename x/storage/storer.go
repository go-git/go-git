// Package storage provides extended storage interfaces for experimental features.
package storage

import (
	"github.com/go-git/go-git/v6/plumbing/compat"
	"github.com/go-git/go-git/v6/plumbing/format/config"
)

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

// ExtensionChecker expands a Storer enabling it to confirm whether it supports
// a given Git extension.
//
// If a repository defines an extension and go-git is unable to confirm whether
// the Storer supports it, the repository won't be opened and an error will be
// returned instead.
type ExtensionChecker interface {
	// SupportsExtension checks whether the underlying Storer supports the given
	// Git extension.
	SupportsExtension(name, value string) bool
}

// CompatTranslatorProvider is implemented by storage backends that support
// the compatObjectFormat extension. It provides access to the compat
// Translator, which handles bidirectional hash mapping between native and
// compat object formats.
//
// This interface is experimental and may cease to exist in future go-git releases.
type CompatTranslatorProvider interface {
	// Translator returns the compat translator, or nil if compatObjectFormat
	// is not enabled.
	Translator() *compat.Translator
}

// CompatMappingCompactor is implemented by storers that can compact persisted
// compatObjectFormat mapping data during repository maintenance.
type CompatMappingCompactor interface {
	// CompactCompatMappings rewrites persisted compat mappings into a more
	// compact form when compatObjectFormat is enabled.
	CompactCompatMappings() error
}
