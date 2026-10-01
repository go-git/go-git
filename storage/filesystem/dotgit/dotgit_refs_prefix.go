package dotgit

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// RefsWithPrefix lazily iterates over the refs Refs returns whose names start
// with prefix, with the same values, in ascending byte-wise name order. Loose
// refs and packed refs are merged as Git's files backend does, with a loose
// ref shadowing a packed one of the same name:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1115
// It holds no file open.
func (d *DotGit) RefsWithPrefix(prefix string) (storer.ReferenceIter, error) {
	loose, err := d.looseRefs(prefix)
	if err != nil {
		return nil, err
	}

	return reference.NewOverlayIter(loose, &packedRefsSource{d: d, prefix: prefix}), nil
}

// looseRefs returns the loose refs matching prefix, starting with HEAD when
// it matches, since HEAD sorts before every name under refs/. A ref Git
// treats as broken is returned with the all-zero ID, as Git's ref store
// reports it.
func (d *DotGit) looseRefs(prefix string) (*looseRefsSource, error) {
	s := &looseRefsSource{d: d}

	if strings.HasPrefix("HEAD", prefix) { //nolint:gocritic // HEAD matches a prefix of itself
		head, err := s.read("HEAD")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		s.head = head
	}

	dir, err := d.looseRefsDirWithPrefix(prefix)
	if err != nil {
		return nil, err
	}
	if dir != nil {
		s.dirs = append(s.dirs, *dir)
	}
	return s, nil
}

// looseRefsDir holds unvisited entries and their slash-separated directory
// path relative to .git.
type looseRefsDir struct {
	name    string
	entries []fs.DirEntry
}

// looseRefsEntries lists the loose refs directory dir, skipping entries named
// ".*" or "*.lock", which Git's ref store never reads as refs: a "*.lock" file
// exists while git update-ref updates a ref. It sorts the rest so that a
// depth-first walk yields names in byte-wise order. Git names a directory
// entry with a trailing slash, so "a-c" sorts before the refs under "a/":
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L386-L398
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/ref-cache.c#L108-L113
func (d *DotGit) looseRefsEntries(dir string) ([]fs.DirEntry, error) {
	entries, err := d.fs.ReadDir(d.fs.Join(strings.Split(dir, "/")...))
	if err != nil {
		return nil, err
	}

	entries = slices.DeleteFunc(entries, func(e fs.DirEntry) bool {
		return strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".lock")
	})
	key := func(e fs.DirEntry) string {
		if e.IsDir() {
			return e.Name() + "/"
		}
		return e.Name()
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		return strings.Compare(key(a), key(b))
	})
	return entries, nil
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
		entries, err := d.looseRefsEntries(dir)
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

// looseRefsSource yields HEAD, then walks loose refs depth-first in sorted
// order.
type looseRefsSource struct {
	d    *DotGit
	head *plumbing.Reference
	dirs []looseRefsDir
}

// read reads the loose ref name. An unparsable file is reported with the
// all-zero ID, as Git's loose-ref loader marks it broken and its ref store
// then reports it:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L330-L343
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs.c#L1859-L1868
func (s *looseRefsSource) read(name string) (*plumbing.Reference, error) {
	ref, err := s.d.readReferenceFile(".", name)
	if errors.Is(err, ErrEmptyRefFile) || errors.Is(err, ErrBrokenRefFile) {
		ref, err = plumbing.NewHashReference(plumbing.ReferenceName(name), plumbing.ZeroHash), nil
	}
	return ref, err
}

func (s *looseRefsSource) Next() (*plumbing.Reference, error) {
	if s.head != nil {
		ref := s.head
		s.head = nil
		return ref, nil
	}

	for len(s.dirs) > 0 {
		dir := &s.dirs[len(s.dirs)-1]
		if len(dir.entries) == 0 {
			s.dirs = s.dirs[:len(s.dirs)-1]
			continue
		}

		entry := dir.entries[0]
		dir.entries = dir.entries[1:]
		name := dir.name + "/" + entry.Name()

		if entry.IsDir() {
			entries, err := s.d.looseRefsEntries(name)
			if os.IsNotExist(err) {
				// The directory may have been removed since listing.
				continue
			}
			if err != nil {
				return nil, err
			}

			s.dirs = append(s.dirs, looseRefsDir{name: name, entries: entries})
			continue
		}

		ref, err := s.read(name)
		if os.IsNotExist(err) {
			// The file may have been removed since listing.
			continue
		}
		if err != nil {
			return nil, err
		}

		return ref, nil
	}

	return nil, io.EOF
}

func (s *looseRefsSource) Close() {
	s.head = nil
	s.dirs = nil
}

// packedRefsSource yields the packed refs matching prefix in sorted order.
// On first use it reads packed-refs whole, so it holds no file open, and
// collects the matches in one pass, sorting them unless the names already
// ascend. It does not rely on the sorted header trait, which Git trusts:
// verifying it costs the same pass, and a false claim would hide matches.
type packedRefsSource struct {
	d      *DotGit
	prefix string
	opened bool
	refs   []*plumbing.Reference
}

func (s *packedRefsSource) Next() (*plumbing.Reference, error) {
	if !s.opened {
		s.opened = true
		if err := s.open(); err != nil {
			return nil, err
		}
	}

	if len(s.refs) == 0 {
		return nil, io.EOF
	}
	ref := s.refs[0]
	s.refs = s.refs[1:]
	return ref, nil
}

func (s *packedRefsSource) open() (err error) {
	f, err := s.d.fs.Open(packedRefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}
	defer ioutil.CheckClose(f, &err)

	// A builder grown to the file's size holds it in one allocation, which
	// String returns without copying.
	var content strings.Builder
	if st, err := s.d.fs.Stat(packedRefsPath); err == nil {
		content.Grow(int(st.Size()))
	}
	if _, err := io.Copy(&content, f); err != nil {
		return err
	}

	sorted, prev := true, ""
	for line := range strings.Lines(content.String()) {
		hash, name, ok, err := parsePackedRefLine(strings.TrimSuffix(line, "\n"))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if name < prev {
			sorted = false
		}
		prev = name

		if strings.HasPrefix(name, s.prefix) {
			// Cloned, so the refs do not keep the whole file alive.
			s.refs = append(s.refs, plumbing.NewReferenceFromStrings(strings.Clone(name), hash))
		}
	}

	if !sorted {
		// Stable, so the first of repeated names stays first, as in Refs.
		slices.SortStableFunc(s.refs, func(a, b *plumbing.Reference) int {
			return strings.Compare(a.Name().String(), b.Name().String())
		})
	}
	return nil
}

func (s *packedRefsSource) Close() {
	s.opened = true
	s.refs = nil
}
