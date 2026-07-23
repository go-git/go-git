package storer

import (
	"context"

	"github.com/go-git/go-billy/v6"
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
	Init(ctx context.Context) error
}

// FilesystemStorer is a storer that can be used to store objects and references
// in a filesystem. It is used by the filesystem storage.
type FilesystemStorer interface {
	Filesystem() billy.Filesystem
}

// IdleReleaser is implemented by storers that can drop idle
// file descriptors (or other I/O resources) without becoming
// unusable. Callers detect via this interface and call
// [IdleReleaser.CloseIdleDescriptors] at a known quiet point —
// for example between reconcile bursts.
//
// Implementations must remain fully usable after the call;
// subsequent operations reopen resources on demand.
type IdleReleaser interface {
	CloseIdleDescriptors() error
}
