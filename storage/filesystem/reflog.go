package filesystem

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ReflogStorage implements storer.ReflogStorer backed by the filesystem.
type ReflogStorage struct {
	dir *dotgit.DotGit
}

// ForEachReflogHash visits object hashes in valid reflog records.
// Malformed records are skipped, as in native Git's repack walk.
func (r *ReflogStorage) ForEachReflogHash(fn func(plumbing.Hash) error) error {
	names, err := r.dir.ReflogNames()
	if err != nil {
		return fmt.Errorf("listing reflogs: %w", err)
	}

	for _, name := range names {
		if err := r.forEachReflogHash(name, fn); err != nil {
			return fmt.Errorf("reading reflog %q: %w", name, err)
		}
	}
	return nil
}

func (r *ReflogStorage) forEachReflogHash(
	name plumbing.ReferenceName,
	fn func(plumbing.Hash) error,
) (err error) {
	f, err := r.dir.ReflogReader(name)
	if err != nil || f == nil {
		return err
	}
	defer ioutil.CheckClose(f, &err)

	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			entry, decodeErr := reflog.NewDecoder(bytes.NewReader(line)).Next()
			if decodeErr == nil {
				for _, hash := range [...]plumbing.Hash{
					entry.OldHash, entry.NewHash,
				} {
					if err := fn(hash); err != nil {
						return err
					}
				}
			}
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
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
