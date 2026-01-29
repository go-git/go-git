// Package storage defines the interfaces for storing objects, references
// and any information related to a particular repository.
package storage

import (
	"errors"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ErrReferenceHasChanged is returned when an atomic compare-and-swap operation fails
// because the reference has changed concurrently.
var ErrReferenceHasChanged = errors.New("reference has changed concurrently")

// Storer is a generic storage of objects, references and any information
// related to a particular repository. The package github.com/go-git/go-git/v6/storage
// contains two implementation a filesystem base implementation (such as `.git`)
// and a memory implementations being ephemeral
type Storer interface {
	storer.EncodedObjectStorer
	storer.ReferenceStorer
	storer.ShallowStorer
	storer.IndexStorer
	config.ConfigStorer
	ModuleStorer
}

// ObjectFormatSetter is implemented by storage backends that support
// configuring the object format (hash algorithm) used for the repository.
type ObjectFormatSetter interface {
	// SetObjectFormat configures the object format (hash algorithm) for this storage.
	SetObjectFormat(formatcfg.ObjectFormat) error
}

// ModuleStorer allows interact with the modules' Storers
type ModuleStorer interface {
	// Module returns a Storer representing a submodule, if not exists returns a
	// new empty Storer is returned
	Module(name string) (Storer, error)
}
