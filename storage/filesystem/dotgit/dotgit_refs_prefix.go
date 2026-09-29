package dotgit

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// RefsWithPrefix lazily iterates over the refs whose names start with prefix,
// in ascending byte-wise name order. Loose refs and packed refs are merged as
// Git's files backend does, with a loose ref shadowing a packed one of the
// same name:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1115
// Like git for-each-ref, it skips broken loose refs, which Refs returns with
// the all-zero ID, and ignores loose entries named ".*" or "*.lock", which Git
// does not read as refs. It holds no file open.
func (d *DotGit) RefsWithPrefix(prefix string) (storer.ReferenceIter, error) {
	return d.refsWithPrefix(prefix, true)
}

// refsWithPrefix merges loose and packed refs matching prefix. forEachRef
// selects git for-each-ref's view of loose refs, as looseRefs describes.
func (d *DotGit) refsWithPrefix(prefix string, forEachRef bool) (storer.ReferenceIter, error) {
	loose, err := d.looseRefs(prefix, forEachRef)
	if err != nil {
		return nil, err
	}

	overlay := reference.NewOverlayIter(loose, &packedRefsSource{d: d, prefix: prefix})
	if !forEachRef {
		return overlay, nil
	}

	// Broken loose refs take part in the merge, so they still hide packed
	// refs of the same name, and are dropped after it, as Git does:
	// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1100-L1115
	// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1028-L1032
	return storer.NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return !loose.broken[r.Name()]
	}, overlay), nil
}

// looseRefs returns the loose refs matching prefix, starting with HEAD when
// it matches, since HEAD sorts before every name under refs/. A ref Git
// treats as broken is returned with the all-zero ID and recorded in broken,
// as Git's ref store reports it. skipNonRefs leaves out the entries Git does
// not read as refs; without it every entry is listed.
func (d *DotGit) looseRefs(prefix string, skipNonRefs bool) (*looseRefsSource, error) {
	s := &looseRefsSource{d: d, skipNonRefs: skipNonRefs, broken: make(map[plumbing.ReferenceName]bool)}

	if strings.HasPrefix("HEAD", prefix) { //nolint:gocritic // HEAD matches a prefix of itself
		head, err := s.read("HEAD")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		s.head = head
	}

	dir, err := d.looseRefsDirWithPrefix(prefix, skipNonRefs)
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

// looseRefsEntries lists the loose refs directory dir, optionally skipping the
// entries Git does not read as refs, and sorts them so that a depth-first
// walk yields names in byte-wise order. Git names a directory entry with a trailing slash, so
// "a-c" sorts before the refs under "a/":
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L386-L398
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/ref-cache.c#L108-L113
func (d *DotGit) looseRefsEntries(dir string, skipNonRefs bool) ([]fs.DirEntry, error) {
	entries, err := d.fs.ReadDir(d.fs.Join(strings.Split(dir, "/")...))
	if err != nil {
		return nil, err
	}

	if skipNonRefs {
		entries = slices.DeleteFunc(entries, func(e fs.DirEntry) bool {
			return strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".lock")
		})
	}
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
func (d *DotGit) looseRefsDirWithPrefix(prefix string, skipNonRefs bool) (*looseRefsDir, error) {
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
		entries, err := d.looseRefsEntries(dir, skipNonRefs)
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
	// skipNonRefs leaves out entries Git does not read as refs.
	skipNonRefs bool
	// broken holds the refs Git treats as broken, yielded with the all-zero ID.
	broken map[plumbing.ReferenceName]bool
}

// read reads the loose ref name. An unparsable file becomes an all-zero
// placeholder, and it and a ref holding the all-zero ID are recorded as
// broken, as Git's loose-ref loader marks them and its ref store then reports
// them with the all-zero ID:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L330-L343
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs.c#L1859-L1868
func (s *looseRefsSource) read(name string) (*plumbing.Reference, error) {
	ref, err := s.d.readReferenceFile(".", name)
	if errors.Is(err, ErrEmptyRefFile) || errors.Is(err, ErrBrokenRefFile) {
		ref, err = plumbing.NewHashReference(plumbing.ReferenceName(name), plumbing.ZeroHash), nil
	}
	if err == nil && ref.Type() == plumbing.HashReference && ref.Hash().IsZero() {
		s.broken[ref.Name()] = true
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
			entries, err := s.d.looseRefsEntries(name, s.skipNonRefs)
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
// It reads packed-refs into memory at once, so it holds no file open, and
// parses it lazily. The sorted header trait permits stopping past the prefix,
// as Git does:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/packed-backend.c#L1015-L1022
// Unlike Git, which trusts the trait, it first checks that the names do
// ascend. For a file that is not sorted, as Git does, it sorts the matches,
// so the first result waits for a full scan.
type packedRefsSource struct {
	d      *DotGit
	prefix string
	opened bool
	// sorted reports that rest, the records not yet read, are sorted.
	sorted bool
	rest   string
	// unsorted holds the matches of a file that is not sorted.
	unsorted []*plumbing.Reference
	done     bool
}

func (s *packedRefsSource) Next() (*plumbing.Reference, error) {
	if s.done {
		return nil, io.EOF
	}

	if !s.opened {
		s.opened = true
		if err := s.open(); err != nil || s.done {
			return nil, err
		}
	}

	if !s.sorted {
		if len(s.unsorted) == 0 {
			s.Close()
			return nil, io.EOF
		}
		ref := s.unsorted[0]
		s.unsorted = s.unsorted[1:]
		return ref, nil
	}

	for s.rest != "" {
		var line string
		line, s.rest, _ = strings.Cut(s.rest, "\n")
		ref, past, err := s.matchLine(line)
		if err != nil {
			return nil, err
		}
		if past {
			break
		}
		if ref != nil {
			return ref, nil
		}
	}

	s.Close()
	return nil, io.EOF
}

// open reads packed-refs and, unless it is sorted, collects and sorts every
// match. It marks the source done if packed-refs is missing.
func (s *packedRefsSource) open() error {
	b, err := util.ReadFile(s.d.fs, packedRefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.done = true
			return nil
		}

		return err
	}
	content := string(b)

	// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/packed-backend.c#L740-L763
	first, rest, _ := strings.Cut(content, "\n")
	if traits, ok := strings.CutPrefix(first, packedRefsHeader); ok &&
		slices.Contains(strings.Split(traits, " "), "sorted") {
		s.sorted = true
		var prev string
		for line := range strings.Lines(rest) {
			if _, name, ok, _ := parsePackedRefLine(strings.TrimSuffix(line, "\n")); ok {
				if name < prev {
					s.sorted = false
					break
				}
				prev = name
			}
		}
	}
	if s.sorted {
		s.rest = rest
		return nil
	}

	// Without a header, the first line is a reference too.
	for line := range strings.Lines(content) {
		ref, _, err := s.matchLine(strings.TrimSuffix(line, "\n"))
		if err != nil {
			return err
		}
		if ref != nil {
			s.unsorted = append(s.unsorted, ref)
		}
	}

	// Stable, so the first of repeated names stays first, as in Refs.
	slices.SortStableFunc(s.unsorted, func(a, b *plumbing.Reference) int {
		return strings.Compare(a.Name().String(), b.Name().String())
	})
	return nil
}

// matchLine returns the reference on line if it matches the prefix, and
// reports past once a name sorts after every match.
func (s *packedRefsSource) matchLine(line string) (ref *plumbing.Reference, past bool, err error) {
	hash, name, ok, err := parsePackedRefLine(line)
	if err != nil || !ok {
		return nil, false, err
	}

	if !strings.HasPrefix(name, s.prefix) {
		return nil, name > s.prefix, nil
	}

	return plumbing.NewReferenceFromStrings(name, hash), false, nil
}

func (s *packedRefsSource) Close() {
	s.done = true
	s.rest = ""
	s.unsorted = nil
}
