package dotgit

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

const packedRefsHeader = "# pack-refs with: "

// RefsWithPrefix lazily iterates over Refs filtered by strings.HasPrefix.
// It yields matching HEAD, loose refs, then unshadowed packed refs.
// The iterator must be closed.
func (d *DotGit) RefsWithPrefix(prefix string) (storer.ReferenceIter, error) {
	iter := &refsWithPrefixIter{
		d:      d,
		prefix: prefix,
		seen:   make(map[plumbing.ReferenceName]bool),
	}

	if strings.HasPrefix("HEAD", prefix) { //nolint:gocritic // HEAD matches a prefix of itself
		head, err := d.readReferenceFile(".", "HEAD")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		iter.head = head
	}

	dir, err := d.looseRefsDirWithPrefix(prefix)
	if err != nil {
		return nil, err
	}
	if dir != nil {
		iter.dirs = append(iter.dirs, *dir)
	}

	return iter, nil
}

// looseRefsDir holds unvisited entries and their slash-separated directory
// path relative to .git.
type looseRefsDir struct {
	name    string
	entries []fs.DirEntry
}

// looseRefsDirWithPrefix returns the deepest directory under refs/ that
// holds every loose reference matching prefix, with its entries filtered to
// those that can match. It returns nil when no loose reference can match.
//
// Looking up components in directory listings prevents traversal through the
// prefix and preserves on-disk spelling: "refs/Heads/" does not match
// "refs/heads/", even on case-insensitive filesystems.
func (d *DotGit) looseRefsDirWithPrefix(prefix string) (*looseRefsDir, error) {
	rest, underRefs := strings.CutPrefix(prefix, refsPath+"/")
	if !underRefs {
		// Every name under refs/ matches a prefix of "refs/", such as "ref".
		if !strings.HasPrefix(refsPath+"/", prefix) { //nolint:gocritic // prefix is tested against "refs/"
			return nil, nil
		}
		rest = ""
	}

	dir := refsPath
	for {
		entries, err := d.fs.ReadDir(d.fs.Join(strings.Split(dir, "/")...))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}

			return nil, err
		}

		component, remainder, isDir := strings.Cut(rest, "/")
		if !isDir {
			entries = slices.DeleteFunc(entries, func(e fs.DirEntry) bool {
				return !strings.HasPrefix(e.Name(), component)
			})

			return &looseRefsDir{name: dir, entries: entries}, nil
		}

		i := slices.IndexFunc(entries, func(e fs.DirEntry) bool {
			return e.IsDir() && e.Name() == component
		})
		if i < 0 {
			return nil, nil
		}

		dir += "/" + component
		rest = remainder
	}
}

type refsWithPrefixIter struct {
	d      *DotGit
	prefix string
	head   *plumbing.Reference

	// dirs is the stack of loose reference directories being walked.
	dirs []looseRefsDir
	// seen holds the references yielded, so that loose ones shadow packed
	// ones and repeated packed-refs entries are yielded once.
	seen map[plumbing.ReferenceName]bool

	packed        billy.File
	packedScanner *bufio.Scanner
	packedSorted  bool
	packedDone    bool
}

// Next returns the next reference, or io.EOF once all have been returned.
func (iter *refsWithPrefixIter) Next() (*plumbing.Reference, error) {
	if iter.head != nil {
		ref := iter.head
		iter.head = nil
		return ref, nil
	}

	ref, err := iter.nextLoose()
	if ref != nil || err != nil {
		return ref, err
	}

	return iter.nextPacked()
}

// nextLoose walks the loose references depth-first, in the same order as
// Refs, and returns nil once none are left.
func (iter *refsWithPrefixIter) nextLoose() (*plumbing.Reference, error) {
	for len(iter.dirs) > 0 {
		dir := &iter.dirs[len(iter.dirs)-1]
		if len(dir.entries) == 0 {
			iter.dirs = iter.dirs[:len(iter.dirs)-1]
			continue
		}

		entry := dir.entries[0]
		dir.entries = dir.entries[1:]
		name := dir.name + "/" + entry.Name()

		if entry.IsDir() {
			entries, err := iter.d.fs.ReadDir(iter.d.fs.Join(strings.Split(name, "/")...))
			if os.IsNotExist(err) {
				// The directory may have been removed since listing.
				continue
			}
			if err != nil {
				return nil, err
			}

			iter.dirs = append(iter.dirs, looseRefsDir{name: name, entries: entries})
			continue
		}

		ref, err := iter.d.readReferenceFile(".", name)
		if os.IsNotExist(err) {
			// The file may have been removed since listing.
			continue
		}
		if err != nil {
			return nil, err
		}

		iter.seen[ref.Name()] = true
		return ref, nil
	}

	return nil, nil
}

// nextPacked returns the next matching, unseen packed reference.
// The sorted header trait permits stopping past the prefix, as Git does:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/packed-backend.c#L1015-L1022
// Otherwise, exhausting the iterator requires scanning the whole file.
func (iter *refsWithPrefixIter) nextPacked() (*plumbing.Reference, error) {
	if iter.packedDone {
		return nil, io.EOF
	}

	if iter.packed == nil {
		f, err := iter.d.fs.Open(packedRefsPath)
		if err != nil {
			if os.IsNotExist(err) {
				iter.packedDone = true
				return nil, io.EOF
			}

			return nil, err
		}

		iter.packed = f
		iter.packedScanner = bufio.NewScanner(f)
		if iter.packedScanner.Scan() {
			// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/packed-backend.c#L740-L763
			if traits, ok := strings.CutPrefix(iter.packedScanner.Text(), packedRefsHeader); ok {
				iter.packedSorted = slices.Contains(strings.Split(traits, " "), "sorted")
			}
			if ref, err := iter.matchPackedLine(iter.packedScanner.Text()); ref != nil || err != nil {
				return ref, err
			}
		}
	}

	for !iter.packedDone && iter.packedScanner.Scan() {
		if ref, err := iter.matchPackedLine(iter.packedScanner.Text()); ref != nil || err != nil {
			return ref, err
		}
	}

	if err := iter.packedScanner.Err(); err != nil {
		return nil, err
	}

	iter.Close()
	return nil, io.EOF
}

// matchPackedLine returns the reference on line if it matches the prefix and
// has not been yielded already. It marks the scan as done once a
// sorted file is past the prefix.
func (iter *refsWithPrefixIter) matchPackedLine(line string) (*plumbing.Reference, error) {
	hash, name, ok, err := parsePackedRefLine(line)
	if err != nil || !ok {
		return nil, err
	}

	if !strings.HasPrefix(name, iter.prefix) {
		if iter.packedSorted && name > iter.prefix {
			iter.packedDone = true
		}
		return nil, nil
	}

	// Like Refs, keep the first of repeated names, whether loose or packed.
	if iter.seen[plumbing.ReferenceName(name)] {
		return nil, nil
	}
	iter.seen[plumbing.ReferenceName(name)] = true

	return plumbing.NewReferenceFromStrings(name, hash), nil
}

// ForEach calls cb for each reference and closes the iterator.
// Returning storer.ErrStop stops iteration without an error.
func (iter *refsWithPrefixIter) ForEach(cb func(*plumbing.Reference) error) error {
	defer iter.Close()
	for {
		ref, err := iter.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := cb(ref); err != nil {
			if errors.Is(err, storer.ErrStop) {
				return nil
			}

			return err
		}
	}
}

// Close releases the packed-refs file, if open. Later calls to Next return
// io.EOF.
func (iter *refsWithPrefixIter) Close() {
	iter.head = nil
	iter.dirs = nil
	iter.packedDone = true
	if iter.packed != nil {
		_ = iter.packed.Close()
		iter.packed = nil
	}
}
