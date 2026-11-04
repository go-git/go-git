package dotgit

import (
	"bufio"
	"bytes"
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
// It holds no file open. Packed refs are read once, when first needed, and
// loose refs as the walk reaches them, so a concurrent update may or may not
// be seen, and a ref renamed during the walk may be yielded twice or not at
// all.
func (d *DotGit) RefsWithPrefix(prefix string) (storer.ReferenceIter, error) {
	loose, err := d.looseRefs(prefix)
	if err != nil {
		return nil, err
	}

	return reference.NewOverlayIter(loose, loose.packed), nil
}

// looseRefs returns the loose refs matching prefix, starting with HEAD when
// it matches, since HEAD sorts before every name under refs/. A ref Git
// treats as broken is returned with the all-zero ID, as Git's ref store
// reports it.
func (d *DotGit) looseRefs(prefix string) (*looseRefsSource, error) {
	s := &looseRefsSource{d: d, packed: &packedRefsSource{d: d, prefix: prefix}}

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
// order. packed holds the packed refs matching the same prefix, which stand
// for empty loose files.
type looseRefsSource struct {
	d      *DotGit
	packed *packedRefsSource
	head   *plumbing.Reference
	dirs   []looseRefsDir
}

// read reads the loose ref name as Ref does, with an empty file standing for
// the packed ref of that name. A file Ref fails as broken is reported with the
// all-zero ID of the repository's object format, as Git's loose-ref loader
// marks it broken and its ref store then reports it:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/files-backend.c#L330-L343
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs.c#L1859-L1868
func (s *looseRefsSource) read(name string) (*plumbing.Reference, error) {
	ref, err := s.d.readReferenceFile(".", name)
	if errors.Is(err, ErrEmptyRefFile) {
		packed, perr := s.packed.lookup(name)
		if perr != nil {
			return nil, perr
		}
		if packed != nil {
			return packed, nil
		}
	}
	if errors.Is(err, ErrEmptyRefFile) || errors.Is(err, ErrBrokenRefFile) {
		zero, _ := plumbing.FromBytes(make([]byte, s.d.options.ObjectFormat.Size()))
		ref, err = plumbing.NewHashReference(plumbing.ReferenceName(name), zero), nil
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
// When first needed it reads packed-refs in one pass and closes it, so it
// holds no file open, collecting the matches and sorting them unless they
// already ascend. It does not rely on the sorted header trait, which Git
// trusts: verifying it costs the same pass, and a false claim would hide
// matches.
type packedRefsSource struct {
	d      *DotGit
	prefix string
	loaded bool
	err    error
	refs   []*plumbing.Reference
	pos    int
}

func (s *packedRefsSource) Next() (*plumbing.Reference, error) {
	if err := s.load(); err != nil {
		return nil, err
	}

	if s.pos >= len(s.refs) {
		return nil, io.EOF
	}
	ref := s.refs[s.pos]
	s.pos++
	return ref, nil
}

// lookup returns the first packed ref named name, which must match prefix,
// or nil when there is none.
func (s *packedRefsSource) lookup(name string) (*plumbing.Reference, error) {
	if err := s.load(); err != nil {
		return nil, err
	}

	i, found := slices.BinarySearchFunc(s.refs, name, func(r *plumbing.Reference, name string) int {
		return strings.Compare(r.Name().String(), name)
	})
	if !found {
		return nil, nil
	}
	return s.refs[i], nil
}

func (s *packedRefsSource) load() error {
	if !s.loaded {
		s.loaded = true
		s.err = s.read()
	}
	return s.err
}

func (s *packedRefsSource) read() (err error) {
	f, err := s.d.fs.Open(packedRefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}
	defer ioutil.CheckClose(f, &err)

	// Scanned line by line, as the other packed-refs readers are, so a
	// record longer than bufio.MaxScanTokenSize fails with
	// bufio.ErrTooLong instead of growing memory to fit it. ScanLines drops
	// a carriage return before the newline, so a CRLF record reads the same
	// everywhere in go-git. Git itself keeps the carriage return in the name
	// and then ignores the record as a ref with a broken name. Only matches
	// are copied out of the scanner's buffer.
	prefix := []byte(s.prefix)
	sorted := true
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), bufio.MaxScanTokenSize)
	for sc.Scan() {
		hash, name, ok, err := s.d.parsePackedRefLine(sc.Bytes())
		if err != nil {
			return err
		}
		if !ok || !bytes.HasPrefix(name, prefix) {
			continue
		}

		// One copy per match, which the name and the ID share.
		line := string(sc.Bytes())
		ref := plumbing.NewReferenceFromStrings(line[len(hash)+1:], line[:len(hash)])
		if n := len(s.refs); n > 0 && ref.Name() < s.refs[n-1].Name() {
			sorted = false
		}
		s.refs = append(s.refs, ref)
	}
	if err := sc.Err(); err != nil {
		return err
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
	s.loaded = true
	s.refs = nil
}
