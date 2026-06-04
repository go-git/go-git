package filesystem

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ReflogStorage implements storer.ReflogStorer backed by the filesystem.
type ReflogStorage struct {
	dir *dotgit.DotGit
}

// Reflog returns all reflog entries for the given reference, oldest first.
func (r *ReflogStorage) Reflog(name plumbing.ReferenceName) ([]*reflog.Entry, error) {
	f, err := r.dir.ReflogReader(name)
	if f == nil || err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(f, &err)
	entries, err := reflog.Decode(f)
	return entries, err
}

// AppendReflog appends a single entry to the reflog for the given reference.
func (r *ReflogStorage) AppendReflog(name plumbing.ReferenceName, entry *reflog.Entry) error {
	f, err := r.dir.ReflogWriter(name)
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)
	return reflog.Encode(f, entry)
}

// DeleteReflog removes the entire reflog for the given reference.
func (r *ReflogStorage) DeleteReflog(name plumbing.ReferenceName) error {
	return r.dir.DeleteReflog(name)
}
