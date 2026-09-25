package dotgit

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const packedRefsHeader = "# pack-refs with: "

// RefsWithPrefix lazily iterates over the refs whose names start with prefix,
// in ascending byte-wise name order. Loose refs and packed refs are merged as
// Git's files backend does, with a loose ref shadowing a packed one of the
// same name:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1115
// Unlike Refs, and like Git, it skips broken loose refs rather than failing,
// and ignores loose entries named ".*" or "*.lock".
// The iterator must be closed.
func (d *DotGit) RefsWithPrefix(prefix string) (storer.ReferenceIter, error) {
	loose := &looseRefsSource{d: d, broken: make(map[*plumbing.Reference]bool)}

	// HEAD sorts before every name under refs/.
	if strings.HasPrefix("HEAD", prefix) { //nolint:gocritic // HEAD matches a prefix of itself
		head, broken, err := d.readLooseRef("HEAD")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if !broken {
			loose.head = head
		}
	}

	dir, err := d.looseRefsDirWithPrefix(prefix)
	if err != nil {
		return nil, err
	}
	if dir != nil {
		loose.dirs = append(loose.dirs, *dir)
	}

	// Broken loose refs take part in the merge, so they still hide packed
	// refs of the same name, and are dropped after it, as Git does:
	// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1100-L1115
	// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L1028-L1032
	overlay := reference.NewOverlayIter(loose, &packedRefsSource{d: d, prefix: prefix})
	return storer.NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return !loose.broken[r]
	}, overlay), nil
}

// looseRefsDir holds unvisited entries and their slash-separated directory
// path relative to .git.
type looseRefsDir struct {
	name    string
	entries []fs.DirEntry
}

// looseRefsEntries lists the loose refs directory dir, skipping the entries
// Git skips, and sorts the rest so that a depth-first walk yields names in
// byte-wise order. Git names a directory entry with a trailing slash, so
// "a-c" sorts before the refs under "a/":
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

// readLooseRef reads the loose ref name as Git parses it, reporting broken
// when Git would skip it as broken: content that is neither "ref:" and a
// target, nor a full object ID followed by whitespace or the end, and the
// all-zero ID:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L668-L697
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L330-L343
func (d *DotGit) readLooseRef(name string) (ref *plumbing.Reference, broken bool, err error) {
	path := d.fs.Join(strings.Split(name, "/")...)
	st, err := d.fs.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if st.IsDir() {
		return nil, false, ErrIsDir
	}

	f, err := d.fs.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer ioutil.CheckClose(f, &err)
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}

	// Git's isspace, unlike C's, excludes \v and \f:
	// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/ctype.c#L21-L23
	const gitSpace = " \t\n\r"
	content := string(b)
	if target, ok := strings.CutPrefix(content, "ref:"); ok {
		target = strings.Trim(target, gitSpace)
		return plumbing.NewSymbolicReference(plumbing.ReferenceName(name), plumbing.ReferenceName(target)), target == "", nil
	}

	size := d.options.ObjectFormat.HexSize()
	ref = plumbing.NewHashReference(plumbing.ReferenceName(name), plumbing.ZeroHash)
	if len(content) < size || !isHex(content[:size]) ||
		(len(content) > size && !strings.ContainsRune(gitSpace, rune(content[size]))) {
		return ref, true, nil
	}

	ref = plumbing.NewHashReference(plumbing.ReferenceName(name), plumbing.NewHash(content[:size]))
	return ref, ref.Hash().IsZero(), nil
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
// order. It yields broken refs too, recording them in broken.
type looseRefsSource struct {
	d      *DotGit
	head   *plumbing.Reference
	dirs   []looseRefsDir
	broken map[*plumbing.Reference]bool
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

		ref, broken, err := s.d.readLooseRef(name)
		if os.IsNotExist(err) {
			// The file may have been removed since listing.
			continue
		}
		if err != nil {
			return nil, err
		}

		if broken {
			s.broken[ref] = true
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
// The sorted header trait permits streaming and stopping past the prefix, as
// Git does:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/packed-backend.c#L1015-L1022
// Without it, Git sorts the whole file; this collects and sorts the matching
// lines, so the first result waits for a full scan.
type packedRefsSource struct {
	d      *DotGit
	prefix string

	file    billy.File
	scanner *bufio.Scanner
	sorted  bool
	// unsorted holds the matches of a file without the sorted trait.
	unsorted []*plumbing.Reference
	done     bool
}

func (s *packedRefsSource) Next() (*plumbing.Reference, error) {
	if s.done {
		return nil, io.EOF
	}

	if s.scanner == nil {
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

	for s.scanner.Scan() {
		ref, past, err := s.matchLine(s.scanner.Text())
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
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}

	s.Close()
	return nil, io.EOF
}

// open reads the header and, for a file without the sorted trait, collects
// and sorts every match. It marks the source done if packed-refs is missing.
func (s *packedRefsSource) open() error {
	f, err := s.d.fs.Open(packedRefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.done = true
			return nil
		}

		return err
	}
	s.file = f
	s.scanner = bufio.NewScanner(f)

	var first string
	if s.scanner.Scan() {
		first = s.scanner.Text()
		// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/packed-backend.c#L740-L763
		if traits, ok := strings.CutPrefix(first, packedRefsHeader); ok {
			s.sorted = slices.Contains(strings.Split(traits, " "), "sorted")
		}
	}
	if err := s.scanner.Err(); err != nil {
		return err
	}

	if s.sorted {
		return nil
	}

	// The first line is a reference when there is no header.
	collect := func(line string) error {
		ref, _, err := s.matchLine(line)
		if ref != nil {
			s.unsorted = append(s.unsorted, ref)
		}
		return err
	}
	if err := collect(first); err != nil {
		return err
	}
	for s.scanner.Scan() {
		if err := collect(s.scanner.Text()); err != nil {
			return err
		}
	}
	if err := s.scanner.Err(); err != nil {
		return err
	}

	// Stable, so the first of repeated names stays first, as in Refs.
	slices.SortStableFunc(s.unsorted, func(a, b *plumbing.Reference) int {
		return strings.Compare(a.Name().String(), b.Name().String())
	})
	_ = s.file.Close()
	s.file = nil
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
	s.unsorted = nil
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}
