package filesystem

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/plumbing/format/reftable"
)

// ReftableReflogStorage implements storer.ReflogStorer backed by a
// reftable stack.
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

	// Reftable returns records newest-first. Process them and stop if we see
	// a deletion tombstone (LogType == 0).
	var active []*reflog.Entry
	for _, rec := range records {
		if rec.LogType == 0 { // deletion tombstone
			break
		}
		entry, err := logRecordToEntry(&rec)
		if err != nil {
			return nil, err
		}
		active = append(active, entry)
	}

	if len(active) == 0 {
		return nil, nil
	}

	// Reverse to return oldest-first.
	entries := make([]*reflog.Entry, len(active))
	for i := range active {
		entries[len(active)-1-i] = active[i]
	}

	return entries, nil
}

// AppendReflog appends a single entry to the reflog for the given reference.
func (r *ReftableReflogStorage) AppendReflog(name plumbing.ReferenceName, entry *reflog.Entry) error {
	_, tzOffset := entry.Committer.When.Zone()
	tzMinutes := int16(tzOffset / 60)

	rec := reftable.LogRecord{
		RefName:  string(name),
		LogType:  1, // update
		OldHash:  entry.OldHash.Bytes(),
		NewHash:  entry.NewHash.Bytes(),
		Name:     entry.Committer.Name,
		Email:    entry.Committer.Email,
		Time:     entry.Committer.When,
		TZOffset: tzMinutes,
		Message:  entry.Message,
	}

	return r.stack.AddLog(rec)
}

// DeleteReflog removes the entire reflog for the given reference by writing
// a deletion tombstone.
func (r *ReftableReflogStorage) DeleteReflog(name plumbing.ReferenceName) error {
	rec := reftable.LogRecord{
		RefName: string(name),
		LogType: 0, // deletion tombstone
		Time:    time.Now(),
	}
	return r.stack.AddLog(rec)
}

func logRecordToEntry(rec *reftable.LogRecord) (*reflog.Entry, error) {
	if len(rec.OldHash) != 20 && len(rec.OldHash) != 32 {
		return nil, fmt.Errorf("reftable: invalid old hash length: %d", len(rec.OldHash))
	}
	if len(rec.NewHash) != 20 && len(rec.NewHash) != 32 {
		return nil, fmt.Errorf("reftable: invalid new hash length: %d", len(rec.NewHash))
	}
	return &reflog.Entry{
		OldHash: plumbing.NewHash(hex.EncodeToString(rec.OldHash)),
		NewHash: plumbing.NewHash(hex.EncodeToString(rec.NewHash)),
		Committer: reflog.Signature{
			Name:  rec.Name,
			Email: rec.Email,
			When:  rec.Time,
		},
		Message: rec.Message,
	}, nil
}
