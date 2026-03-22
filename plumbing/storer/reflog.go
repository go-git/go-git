package storer

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
)

// ReflogStorer is a storage of reflog entries for references.
type ReflogStorer interface {
	// Reflog returns the reflog entries for the given reference,
	// ordered from oldest to newest.
	Reflog(name plumbing.ReferenceName) ([]*reflog.Entry, error)

	// AppendReflog appends a single entry to the reflog for the given reference.
	AppendReflog(name plumbing.ReferenceName, entry *reflog.Entry) error

	// DeleteReflog removes the entire reflog for the given reference.
	DeleteReflog(name plumbing.ReferenceName) error
}
