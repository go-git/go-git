package filesystem

import (
	"encoding/hex"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/plumbing/format/reftable"
)

// ReftableReflogStorage implements storer.ReflogStorer backed by a
// reftable stack. Currently read-only.
type ReftableReflogStorage struct {
	stack *reftable.Stack
}

// Reflog returns the reflog entries for the given reference, ordered from
// oldest to newest.
func (r *ReftableReflogStorage) Reflog(name plumbing.ReferenceName) ([]*reflog.Entry, error) {
	records, err := r.stack.LogsFor(string(name))
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	// Reftable returns records newest-first, but the ReflogStorer interface
	// expects oldest-first. Reverse the slice.
	entries := make([]*reflog.Entry, len(records))
	for i, rec := range records {
		entries[len(records)-1-i] = logRecordToEntry(&rec)
	}

	return entries, nil
}

// AppendReflog is not supported for reftable (read-only).
func (r *ReftableReflogStorage) AppendReflog(_ plumbing.ReferenceName, _ *reflog.Entry) error {
	return reftable.ErrReadOnly
}

// DeleteReflog is not supported for reftable (read-only).
func (r *ReftableReflogStorage) DeleteReflog(_ plumbing.ReferenceName) error {
	return reftable.ErrReadOnly
}

func logRecordToEntry(rec *reftable.LogRecord) *reflog.Entry {
	return &reflog.Entry{
		OldHash: plumbing.NewHash(hex.EncodeToString(rec.OldHash)),
		NewHash: plumbing.NewHash(hex.EncodeToString(rec.NewHash)),
		Committer: reflog.Signature{
			Name:  rec.Name,
			Email: rec.Email,
			When:  rec.Time,
		},
		Message: rec.Message,
	}
}
