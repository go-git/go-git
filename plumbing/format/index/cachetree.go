package index

import (
	"bytes"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// InvalidateCache invalidates the part of the cache-tree that covers path,
// as Git's cache_tree_invalidate_path does: the root and every directory
// above path are marked invalid, and a cached directory that is named by
// path itself is removed, since path now is an entry of its parent.
//
// Call it whenever an entry at path is added, removed, replaced or changed
// in a way that alters the tree it belongs to (its hash, its mode or its
// stage). It does nothing for an index without a cache-tree.
func (i *Index) InvalidateCache(path string) {
	if i.Cache == nil {
		return
	}

	es := i.Cache.Entries
	if len(es) == 0 || es[0].Path != "" {
		i.Cache = nil
		return
	}

	invalidate(&es[0])

	path = strings.Trim(path, "/")
	if path == "" {
		return
	}

	parts := strings.Split(path, "/")
	dir := 0
	for _, name := range parts[:len(parts)-1] {
		child := childOf(es, dir, name)
		if child < 0 {
			return
		}
		invalidate(&es[child])
		dir = child
	}

	if child := childOf(es, dir, parts[len(parts)-1]); child >= 0 {
		end := subtreeEnd(es, child)
		if end < 0 {
			i.Cache = nil
			return
		}
		es[dir].Trees--
		i.Cache.Entries = slices.Delete(es, child, end)
	}
}

func invalidate(e *TreeEntry) {
	e.Entries = -1
	e.Hash = plumbing.ZeroHash
}

// childOf returns the position in es of the subtree called name of the tree
// at dir, or -1.
func childOf(es []TreeEntry, dir int, name string) int {
	pos := dir + 1
	for range es[dir].Trees {
		if pos >= len(es) {
			return -1
		}
		if es[pos].Path == name {
			return pos
		}
		end := subtreeEnd(es, pos)
		if end < 0 {
			return -1
		}
		pos = end
	}
	return -1
}

// subtreeEnd returns the position after the subtree that starts at start in
// the pre-order list es, or -1 if the list ends inside it or has an entry
// with a negative subtree count.
func subtreeEnd(es []TreeEntry, start int) int {
	pending := 1
	for pos := start; pos < len(es); pos++ {
		if es[pos].Trees < 0 {
			return -1
		}
		pending += es[pos].Trees - 1
		if pending == 0 {
			return pos + 1
		}
		if pending < 0 {
			return -1
		}
	}
	return -1
}

// PruneCache invalidates every cache-tree entry that does not describe the
// entries of the index, so that a cache-tree that went stale cannot be
// trusted or written. An entry is kept only if the tree that the entries
// below its directory make has the hash it records and covers as many
// entries as it says, and so are all the directories below it. A cache-tree
// that is not a well-formed tree is dropped.
//
// It is the safety net behind InvalidateCache for code that changes
// entries in place; it costs hashing the directories of the index.
func (i *Index) PruneCache() {
	if i.Cache == nil {
		return
	}

	es := i.Cache.Entries
	if len(es) == 0 || es[0].Path != "" || subtreeEnd(es, 0) != len(es) {
		i.Cache = nil
		return
	}

	sums, ok := i.sumTrees()
	if !ok {
		i.Cache = nil
		return
	}

	// A frame's path is built only while it names a directory of the index
	// (known), as a cache-tree nested far deeper than the index would
	// otherwise cost memory quadratic in its depth.
	type frame struct {
		pos, next  int
		path       string
		known      bool
		childrenOK bool
		names      map[string]struct{}
	}

	// Entries are visited in pre-order and settled in post-order: a
	// directory stays valid only if its own hash matches and all its
	// subtrees stayed valid.
	valid := make([]bool, len(es))
	stack := []frame{{pos: 0, next: 0, known: true, childrenOK: true}}
	pos := 1
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next < es[f.pos].Trees {
			f.next++
			name := es[pos].Path
			if name == "" || strings.Contains(name, "/") {
				i.Cache = nil
				return
			}
			if f.names == nil {
				f.names = map[string]struct{}{}
			}
			if _, dup := f.names[name]; dup {
				i.Cache = nil
				return
			}
			f.names[name] = struct{}{}
			child := frame{pos: pos, childrenOK: true}
			if f.known {
				child.path = joinPath(f.path, name)
				_, child.known = sums[child.path]
			}
			stack = append(stack, child)
			pos++
			continue
		}

		e := es[f.pos]
		var s *treeSum
		if f.known {
			s = sums[f.path]
		}
		ok := f.childrenOK && e.Entries >= 0 && s != nil && s.valid &&
			s.entries == e.Entries && s.subdirs == e.Trees && s.hash.Equal(e.Hash)
		valid[f.pos] = ok
		stack = stack[:len(stack)-1]
		if len(stack) > 0 && !ok {
			stack[len(stack)-1].childrenOK = false
		}
	}

	for pos := range es {
		if !valid[pos] {
			invalidate(&es[pos])
		}
	}
}

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

type treeSum struct {
	hash    plumbing.Hash
	entries int
	subdirs int
	valid   bool
}

type treeItem struct {
	name string
	mode filemode.FileMode
	hash plumbing.Hash
	dir  bool
}

type treeAcc struct {
	items   []treeItem
	subdirs map[string]string // name to path of the directory
	entries int
	invalid bool
	depth   int // number of components of the directory's path
}

// sumTrees computes, for every directory of the index, the hash of the tree
// that its entries make and the number of entries it covers. A directory
// whose entries cannot make a tree that Git would write (a conflict, an
// intent to add, an entry without an object, a mode Git does not write, or
// a file and a directory of the same name) is not valid. It reports false
// when the index has no entry to take the object format from.
func (i *Index) sumTrees() (map[string]*treeSum, bool) {
	var size int
	for _, e := range i.Entries {
		if !e.Hash.IsZero() {
			size = e.Hash.Size()
			break
		}
	}
	if size == 0 {
		return nil, false
	}
	of := format.SHA1
	if size == format.SHA256Size {
		of = format.SHA256
	}

	accs := map[string]*treeAcc{"": {subdirs: map[string]string{}}}
	acc := func(dir string, depth int) *treeAcc {
		a, ok := accs[dir]
		if !ok {
			a = &treeAcc{subdirs: map[string]string{}, depth: depth}
			accs[dir] = a
		}
		return a
	}

	for _, e := range i.Entries {
		bad := e.Stage != 0 || e.IntentToAdd || e.Hash.IsZero() || e.Hash.Size() != size ||
			(e.Mode != filemode.Regular && e.Mode != filemode.Executable &&
				e.Mode != filemode.Symlink && e.Mode != filemode.Submodule)

		// A directory is keyed by a prefix of the entry name; joining the
		// components would cost memory quadratic in the depth.
		dir, start, depth := "", 0, 0
		for {
			n := strings.IndexByte(e.Name[start:], '/')
			if n < 0 {
				break
			}
			end := start + n
			a := acc(dir, depth)
			a.entries++
			a.invalid = a.invalid || bad
			if _, ok := a.subdirs[e.Name[start:end]]; !ok {
				a.subdirs[e.Name[start:end]] = e.Name[:end]
			}
			dir, start, depth = e.Name[:end], end+1, depth+1
		}
		a := acc(dir, depth)
		a.entries++
		a.invalid = a.invalid || bad
		a.items = append(a.items, treeItem{name: e.Name[start:], mode: e.Mode, hash: e.Hash})
	}

	dirs := make([]string, 0, len(accs))
	for d := range accs {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(a, b int) bool { return accs[dirs[a]].depth > accs[dirs[b]].depth })

	sums := make(map[string]*treeSum, len(accs))
	for _, d := range dirs {
		a := accs[d]
		s := &treeSum{entries: a.entries, subdirs: len(a.subdirs), valid: !a.invalid}
		sums[d] = s

		items := a.items
		for _, it := range a.items {
			if _, clash := a.subdirs[it.name]; clash {
				s.valid = false
			}
		}
		if !s.valid {
			continue
		}
		for name, path := range a.subdirs {
			sub, ok := sums[path]
			if !ok || !sub.valid {
				s.valid = false
				break
			}
			items = append(items, treeItem{name: name, mode: filemode.Dir, hash: sub.hash, dir: true})
		}
		if !s.valid {
			continue
		}

		sort.Slice(items, func(x, y int) bool { return treeSortName(items[x]) < treeSortName(items[y]) })
		var body bytes.Buffer
		for _, it := range items {
			body.WriteString(strconv.FormatUint(uint64(it.mode), 8))
			body.WriteByte(' ')
			body.WriteString(it.name)
			body.WriteByte(0)
			body.Write(it.hash.Bytes())
		}
		h := plumbing.NewHasher(of, plumbing.TreeObject, int64(body.Len()))
		_, _ = h.Write(body.Bytes())
		s.hash = h.Sum()
	}

	return sums, true
}

// treeSortName is the name Git orders tree entries by: a directory sorts as
// if its name ended in a slash.
func treeSortName(it treeItem) string {
	if it.dir {
		return it.name + "/"
	}
	return it.name
}

// FillCache replaces the cache-tree with one that covers the whole index,
// provided the tree that the entries make has the hash root. It reports
// whether it did; when the entries make another tree, or cannot make one Git
// would write, the cache-tree is left as it was.
//
// A cache-tree entry says that the tree object exists, so call FillCache
// only for a root whose tree objects are all in the object database, such as
// the tree of a commit that was just written or checked out.
func (i *Index) FillCache(root plumbing.Hash) bool {
	sums, ok := i.sumTrees()
	if !ok {
		return false
	}
	top, ok := sums[""]
	if !ok || !top.valid || !top.hash.Equal(root) {
		return false
	}

	children := make(map[string][]string, len(sums))
	for dir, s := range sums {
		if !s.valid {
			return false
		}
		if dir == "" {
			continue
		}
		parent, name := "", dir
		if n := strings.LastIndexByte(dir, '/'); n >= 0 {
			parent, name = dir[:n], dir[n+1:]
		}
		children[parent] = append(children[parent], name)
	}
	for _, names := range children {
		sort.Strings(names)
	}

	entries := make([]TreeEntry, 0, len(sums))
	var add func(dir, name string)
	add = func(dir, name string) {
		s := sums[dir]
		entries = append(entries, TreeEntry{Path: name, Entries: s.entries, Trees: s.subdirs, Hash: s.hash})
		for _, child := range children[dir] {
			add(joinPath(dir, child), child)
		}
	}
	add("", "")

	i.Cache = &Tree{Entries: entries}
	return true
}
