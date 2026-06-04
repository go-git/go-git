package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
)

const (
	maxTreeDepth      = 1024
	startingStackSize = 8
)

// New errors defined by this package.
var (
	ErrMaxTreeDepth      = errors.New("maximum tree depth exceeded")
	ErrFileNotFound      = errors.New("file not found")
	ErrDirectoryNotFound = errors.New("directory not found")
	ErrEntryNotFound     = errors.New("entry not found")
	ErrEntriesNotSorted  = errors.New("entries in tree are not sorted")
	ErrMalformedTree     = errors.New("malformed tree")
	ErrDuplicateEntry    = errors.New("duplicate entry in tree")
	ErrInvalidTree       = errors.New("invalid tree")
)

// maxTreeEntryNameLen mirrors the default of upstream Git's
// `fsck.treeEntryLargeName.maxTreeEntryLen` configuration (fsck.c
// in v2.54.0[1]). 4096 bytes is well above any realistic tree-entry
// name; entries longer than this almost always indicate a malformed
// or hand-crafted tree object.
//
// [1]: https://github.com/git/git/blob/v2.54.0/fsck.c#L26
const maxTreeEntryNameLen = 4096

// Tree is basically like a directory - it references a bunch of other trees
// and/or blobs (i.e. files and sub-directories)
type Tree struct {
	Entries []TreeEntry
	Hash    plumbing.Hash

	s             storer.EncodedObjectStorer
	t             map[string]*Tree // tree path cache
	entriesSorted bool
}

// GetTree gets a tree from an object storer and decodes it.
func GetTree(s storer.EncodedObjectStorer, h plumbing.Hash) (*Tree, error) {
	o, err := s.EncodedObject(plumbing.TreeObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeTree(s, o)
}

// DecodeTree decodes an encoded object into a *Tree and associates it to the
// given object storer.
func DecodeTree(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (*Tree, error) {
	t := &Tree{s: s}
	if err := t.Decode(o); err != nil {
		return nil, err
	}

	return t, nil
}

// TreeEntry represents a file
type TreeEntry struct {
	Name string
	Mode filemode.FileMode
	Hash plumbing.Hash
}

// File returns the hash of the file identified by the `path` argument.
// The path is interpreted as relative to the tree receiver.
func (t *Tree) File(path string) (*File, error) {
	e, err := t.FindEntry(path)
	if err != nil {
		return nil, ErrFileNotFound
	}

	blob, err := GetBlob(t.s, e.Hash)
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}

	return NewFile(path, e.Mode, blob), nil
}

// Size returns the plaintext size of an object, without reading it
// into memory.
func (t *Tree) Size(path string) (int64, error) {
	e, err := t.FindEntry(path)
	if err != nil {
		return 0, ErrEntryNotFound
	}

	return t.s.EncodedObjectSize(e.Hash)
}

// Tree returns the tree identified by the `path` argument.
// The path is interpreted as relative to the tree receiver.
func (t *Tree) Tree(path string) (*Tree, error) {
	e, err := t.FindEntry(path)
	if err != nil {
		return nil, ErrDirectoryNotFound
	}

	tree, err := GetTree(t.s, e.Hash)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, ErrDirectoryNotFound
	}

	return tree, err
}

// TreeEntryFile returns the *File for a given *TreeEntry.
//
// The entry's name is validated against pathutil.ValidTreePath for
// the same reason FindEntry validates: TreeEntryFile is a boundary
// where attacker-controlled tree data leaves the trusted store as a
// *File whose Name a caller can hand to filesystem ops.
func (t *Tree) TreeEntryFile(e *TreeEntry) (*File, error) {
	if err := pathutil.ValidTreePath(e.Name); err != nil {
		return nil, err
	}

	blob, err := GetBlob(t.s, e.Hash)
	if err != nil {
		return nil, err
	}

	return NewFile(e.Name, e.Mode, blob), nil
}

// FindEntry search a TreeEntry in this tree or any subtree.
//
// The lookup path is validated against pathutil.ValidTreePath to
// prevent attacker-controlled tree contents from leaking past this
// boundary as `.git`-shaped or path-traversal-shaped names. Callers
// that legitimately need to look up unsafe paths should walk the
// tree manually.
func (t *Tree) FindEntry(path string) (*TreeEntry, error) {
	if err := pathutil.ValidTreePath(path); err != nil {
		return nil, err
	}
	if t.t == nil {
		t.t = make(map[string]*Tree)
	}

	pathParts := strings.Split(path, "/")
	startingTree := t
	pathCurrent := ""

	// search for the longest path in the tree path cache
	for i := len(pathParts) - 1; i >= 1; i-- {
		path := filepath.Join(pathParts[:i]...)

		tree, ok := t.t[path]
		if ok {
			startingTree = tree
			pathParts = pathParts[i:]
			pathCurrent = path

			break
		}
	}

	var tree *Tree
	var err error
	for tree = startingTree; len(pathParts) > 1; pathParts = pathParts[1:] {
		if tree, err = tree.dir(pathParts[0]); err != nil {
			return nil, err
		}

		pathCurrent = filepath.Join(pathCurrent, pathParts[0])
		t.t[pathCurrent] = tree
	}

	return tree.entry(pathParts[0])
}

func (t *Tree) dir(baseName string) (*Tree, error) {
	entry, err := t.entry(baseName)
	if err != nil {
		return nil, ErrDirectoryNotFound
	}

	obj, err := t.s.EncodedObject(plumbing.TreeObject, entry.Hash)
	if err != nil {
		return nil, err
	}

	tree := &Tree{s: t.s}
	err = tree.Decode(obj)

	return tree, err
}

func (t *Tree) entry(baseName string) (*TreeEntry, error) {
	if t.entriesSorted {
		if entry := t.searchEntry(baseName); entry != nil {
			return entry, nil
		}
		return nil, ErrEntryNotFound
	}

	pastName := baseName + "/"
	for i := range t.Entries {
		entry := &t.Entries[i]
		if entry.Name == baseName {
			return entry, nil
		}
		if treeEntrySortName(entry) > pastName {
			break
		}
	}

	return nil, ErrEntryNotFound
}

func (t *Tree) searchEntry(baseName string) *TreeEntry {
	if i := t.searchEntryIndex(baseName); i < len(t.Entries) && t.Entries[i].Name == baseName {
		return &t.Entries[i]
	}

	if i := t.searchEntryIndex(baseName + "/"); i < len(t.Entries) && t.Entries[i].Name == baseName {
		return &t.Entries[i]
	}

	return nil
}

func (t *Tree) searchEntryIndex(name string) int {
	return sort.Search(len(t.Entries), func(i int) bool {
		return treeEntrySortName(&t.Entries[i]) >= name
	})
}

// Files returns a FileIter allowing to iterate over the Tree
func (t *Tree) Files() *FileIter {
	return NewFileIter(t.s, t)
}

// ID returns the object ID of the tree. The returned value will always match
// the current value of Tree.Hash.
//
// ID is present to fulfill the Object interface.
func (t *Tree) ID() plumbing.Hash {
	return t.Hash
}

// Type returns the type of object. It always returns plumbing.TreeObject.
func (t *Tree) Type() plumbing.ObjectType {
	return plumbing.TreeObject
}

func (t *Tree) reset() {
	storer := t.s
	*t = Tree{s: storer}
}

// Decode transform an plumbing.EncodedObject into a Tree struct
func (t *Tree) Decode(o plumbing.EncodedObject) (err error) {
	if o.Type() != plumbing.TreeObject {
		return ErrUnsupportedObject
	}

	t.reset()
	t.Hash = o.Hash()
	// assume tree is sorted as a valid tree should always be sorted.
	t.entriesSorted = true
	if o.Size() == 0 {
		return nil
	}

	reader, err := o.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(reader, &err)

	r := sync.GetBufioReader(reader)
	defer sync.PutBufioReader(r)

	var prevSortName string
	for {
		str, err := r.ReadString(' ')
		if err != nil {
			if err == io.EOF {
				if len(str) != 0 {
					return fmt.Errorf("%w: missing mode terminator", ErrMalformedTree)
				}
				break
			}

			return err
		}
		str = str[:len(str)-1] // strip last byte (' ')

		mode, err := filemode.New(str)
		if err != nil {
			return fmt.Errorf("%w: malformed mode", ErrMalformedTree)
		}
		mode = canonicalTreeMode(mode)

		name, err := r.ReadString(0)
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("%w: missing filename terminator", ErrMalformedTree)
			}
			return err
		}
		if len(name) == 1 {
			return fmt.Errorf("%w: empty filename", ErrMalformedTree)
		}

		var hash plumbing.Hash
		hash.ResetBySize(t.Hash.Size())
		if _, err = hash.ReadFrom(r); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return fmt.Errorf("%w: truncated object id", ErrMalformedTree)
			}
			return err
		}

		baseName := name[:len(name)-1]
		entry := TreeEntry{
			Hash: hash,
			Mode: mode,
			Name: baseName,
		}
		sortName := treeEntrySortName(&entry)
		if len(t.Entries) != 0 && prevSortName > sortName {
			t.entriesSorted = false
		}
		prevSortName = sortName
		t.Entries = append(t.Entries, entry)
	}

	return nil
}

// TreeEntrySorter is a helper type for sorting TreeEntry slices.
type TreeEntrySorter []TreeEntry

func (s TreeEntrySorter) Len() int {
	return len(s)
}

func (s TreeEntrySorter) Less(i, j int) bool {
	return treeEntrySortName(&s[i]) < treeEntrySortName(&s[j])
}

func (s TreeEntrySorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Git compares tree entries as if directory names had a trailing slash.
func treeEntrySortName(e *TreeEntry) string {
	if e.Mode == filemode.Dir {
		return e.Name + "/"
	}
	return e.Name
}

func canonicalTreeMode(mode filemode.FileMode) filemode.FileMode {
	switch mode & 0o170000 {
	case 0o040000:
		return filemode.Dir
	case 0o100000:
		if mode&0o111 != 0 {
			return filemode.Executable
		}
		return filemode.Regular
	case 0o120000:
		return filemode.Symlink
	default:
		return filemode.Submodule
	}
}

// Encode transforms a Tree into a plumbing.EncodedObject.
//
// The tree is run through Tree.Validate before any bytes are written,
// so the encoder cannot produce a tree object containing components
// such as ".git", "..", control characters, HFS+/NTFS variants of
// ".git", null entry hashes, oversize names, mis-sorted or duplicate
// entries, or symlinks disguised as ".gitmodules"/".gitattributes"/
// ".gitignore"/".mailmap". Callers that need to emit such bytes for
// testing or recovery should write them directly via
// plumbing.EncodedObject rather than through this method.
func (t *Tree) Encode(o plumbing.EncodedObject) (err error) {
	if err := t.Validate(); err != nil {
		return err
	}

	o.SetType(plumbing.TreeObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(w, &err)

	for _, entry := range t.Entries {
		if _, err = fmt.Fprintf(w, "%o %s", entry.Mode, entry.Name); err != nil {
			return err
		}

		if _, err = w.Write([]byte{0x00}); err != nil {
			return err
		}

		if _, err = entry.Hash.WriteTo(w); err != nil {
			return err
		}
	}

	return err
}

// Validate reports whether the tree object obeys the same structural
// rules upstream Git's fsck_tree[1] enforces. It is the read-side
// counterpart to Tree.Encode's producer-side gate: Decode is permissive
// so that inspection and recovery tools can read trees with unusual
// entries, and callers that want fsck-shaped reporting call Validate.
//
// The returned error wraps ErrInvalidTree (and, where applicable,
// ErrEntriesNotSorted, ErrDuplicateEntry, or pathutil.ErrInvalidPath)
// so callers can match either the umbrella or specific rule with
// errors.Is. When multiple rules are violated they are reported
// together via errors.Join.
//
// Two fsck_tree warnings — zero-padded modes and the non-canonical
// 0100664 bits — are not surfaced here. Both rely on inspecting the
// original octal string from the wire, which canonicalTreeMode
// discards during Decode. Detecting them would require a parallel
// raw-mode field on TreeEntry; the structural rules below are the
// load-bearing ones for refusing malformed trees.
//
// [1]: https://github.com/git/git/blob/v2.54.0/fsck.c#L616-L800
func (t *Tree) Validate() error {
	var errs []error
	add := func(err error) {
		errs = append(errs, fmt.Errorf("%w: %w", ErrInvalidTree, err))
	}

	seen := make(map[string]struct{}, len(t.Entries))
	var prevSortName string

	for i := range t.Entries {
		e := &t.Entries[i]

		if e.Hash.IsZero() {
			add(fmt.Errorf("entry %q points to null hash", e.Name))
		}

		switch {
		case e.Name == "":
			add(errors.New("contains empty entry name"))
		case strings.ContainsRune(e.Name, '/'):
			add(fmt.Errorf("entry name %q contains a slash", e.Name))
		default:
			if err := pathutil.ValidTreePath(e.Name); err != nil {
				add(err)
			}
			if _, dup := seen[e.Name]; dup {
				add(fmt.Errorf("%w: %q", ErrDuplicateEntry, e.Name))
			}
			seen[e.Name] = struct{}{}

			if len(e.Name) > maxTreeEntryNameLen {
				add(fmt.Errorf("entry name length %d exceeds %d", len(e.Name), maxTreeEntryNameLen))
			}
		}

		// Mode validation against the canonical set. Decode normalises
		// the wire bytes via canonicalTreeMode, so this rule mainly
		// catches programmatically-built trees with garbage modes;
		// the zero-padded-mode and non-canonical-bit checks fsck_tree
		// runs against the raw wire form are out of reach without
		// retaining the original octal string.
		if !isValidTreeMode(e.Mode) {
			add(fmt.Errorf("entry %q has bad mode %o", e.Name, e.Mode))
		}

		// Symlink-disguised metadata files. Mirrors the four FSCK_MSG_*
		// _SYMLINK reports in fsck_tree.
		if e.Mode == filemode.Symlink {
			switch {
			case pathutil.IsHFSDotGitmodules(e.Name) || pathutil.IsNTFSDotGitmodules(e.Name):
				add(errors.New(".gitmodules is a symlink"))
			case pathutil.IsHFSDotGitattributes(e.Name) || pathutil.IsNTFSDotGitattributes(e.Name):
				add(errors.New(".gitattributes is a symlink"))
			case pathutil.IsHFSDotGitignore(e.Name) || pathutil.IsNTFSDotGitignore(e.Name):
				add(errors.New(".gitignore is a symlink"))
			case pathutil.IsHFSDotMailmap(e.Name) || pathutil.IsNTFSDotMailmap(e.Name):
				add(errors.New(".mailmap is a symlink"))
			}
		}

		sortName := treeEntrySortName(e)
		if i > 0 && prevSortName > sortName {
			add(ErrEntriesNotSorted)
		}
		prevSortName = sortName
	}

	return errors.Join(errs...)
}

// isValidTreeMode reports whether mode is one of the canonical tree
// modes upstream Git accepts in fsck_tree, including the non-canonical
// 0100664 that upstream tolerates outside --strict mode.
func isValidTreeMode(mode filemode.FileMode) bool {
	switch mode {
	case filemode.Regular,
		filemode.Executable,
		filemode.Symlink,
		filemode.Dir,
		filemode.Submodule,
		filemode.Deprecated:
		return true
	}
	return false
}

// Diff returns a list of changes between this tree and the provided one
func (t *Tree) Diff(to *Tree) (Changes, error) {
	return t.DiffContext(context.Background(), to)
}

// DiffContext returns a list of changes between this tree and the provided one
// Error will be returned if context expires. Provided context must be non nil.
//
// NOTE: Since version 5.1.0 the renames are correctly handled, the settings
// used are the recommended options DefaultDiffTreeOptions.
func (t *Tree) DiffContext(ctx context.Context, to *Tree) (Changes, error) {
	return DiffTreeWithOptions(ctx, t, to, DefaultDiffTreeOptions)
}

// Patch returns a slice of Patch objects with all the changes between trees
// in chunks. This representation can be used to create several diff outputs.
func (t *Tree) Patch(to *Tree) (*Patch, error) {
	return t.PatchContext(context.Background(), to)
}

// PatchContext returns a slice of Patch objects with all the changes between
// trees in chunks. This representation can be used to create several diff
// outputs. If context expires, an error will be returned. Provided context must
// be non-nil.
//
// NOTE: Since version 5.1.0 the renames are correctly handled, the settings
// used are the recommended options DefaultDiffTreeOptions.
func (t *Tree) PatchContext(ctx context.Context, to *Tree) (*Patch, error) {
	changes, err := t.DiffContext(ctx, to)
	if err != nil {
		return nil, err
	}

	return changes.PatchContext(ctx)
}

// treeEntryIter facilitates iterating through the TreeEntry objects in a Tree.
type treeEntryIter struct {
	t   *Tree
	pos int
}

func (iter *treeEntryIter) Next() (TreeEntry, error) {
	if iter.pos >= len(iter.t.Entries) {
		return TreeEntry{}, io.EOF
	}
	iter.pos++
	return iter.t.Entries[iter.pos-1], nil
}

// TreeWalker provides a means of walking through all of the entries in a Tree.
type TreeWalker struct {
	stack     []*treeEntryIter
	base      string
	recursive bool
	seen      map[plumbing.Hash]bool

	s storer.EncodedObjectStorer
	t *Tree
}

// NewTreeWalker returns a new TreeWalker for the given tree.
//
// It is the caller's responsibility to call Close() when finished with the
// tree walker.
func NewTreeWalker(t *Tree, recursive bool, seen map[plumbing.Hash]bool) *TreeWalker {
	stack := make([]*treeEntryIter, 0, startingStackSize)
	stack = append(stack, &treeEntryIter{t, 0})

	return &TreeWalker{
		stack:     stack,
		recursive: recursive,
		seen:      seen,

		s: t.s,
		t: t,
	}
}

// Next returns the next object from the tree. Objects are returned in order
// and subtrees are included. After the last object has been returned further
// calls to Next() will return io.EOF.
//
// Each entry's name is validated against pathutil.ValidTreePath as it
// surfaces, so callers that funnel the returned name into filesystem
// or archive output can trust it is free of `.git`-shaped components,
// HFS+/NTFS variants, Windows reserved names, and traversal sequences.
// A malformed entry stops the walk with the validator's error;
// inspection-only callers that need to enumerate raw, unvalidated
// names can read Tree.Entries directly.
//
// In the current implementation any objects which cannot be found in the
// underlying repository will be skipped automatically. It is possible that this
// may change in future versions.
func (w *TreeWalker) Next() (name string, entry TreeEntry, err error) {
	var obj *Tree
	for {
		current := len(w.stack) - 1
		if current < 0 {
			// Nothing left on the stack so we're finished
			err = io.EOF
			return name, entry, err
		}

		if current > maxTreeDepth {
			// We're probably following bad data or some self-referencing tree
			err = ErrMaxTreeDepth
			return name, entry, err
		}

		entry, err = w.stack[current].Next()
		if err == io.EOF {
			// Finished with the current tree, move back up to the parent
			w.stack = w.stack[:current]
			w.base, _ = path.Split(w.base)
			w.base = strings.TrimSuffix(w.base, "/")
			continue
		}

		if err != nil {
			return name, entry, err
		}

		if w.seen[entry.Hash] {
			continue
		}

		if err := pathutil.ValidTreePath(entry.Name); err != nil {
			return name, entry, err
		}

		if entry.Mode == filemode.Dir {
			obj, err = GetTree(w.s, entry.Hash)
		}

		name = simpleJoin(w.base, entry.Name)

		if err != nil {
			err = io.EOF
			return name, entry, err
		}

		break
	}

	if !w.recursive {
		return name, entry, err
	}

	if obj != nil {
		w.stack = append(w.stack, &treeEntryIter{obj, 0})
		w.base = simpleJoin(w.base, entry.Name)
	}

	return name, entry, err
}

// Tree returns the tree that the tree walker most recently operated on.
func (w *TreeWalker) Tree() *Tree {
	current := len(w.stack) - 1
	if w.stack[current].pos == 0 {
		current--
	}

	if current < 0 {
		return nil
	}

	return w.stack[current].t
}

// Close releases any resources used by the TreeWalker.
func (w *TreeWalker) Close() {
	w.stack = nil
}

// TreeIter provides an iterator for a set of trees.
type TreeIter struct {
	storer.EncodedObjectIter
	s storer.EncodedObjectStorer
}

// NewTreeIter takes a storer.EncodedObjectStorer and a
// storer.EncodedObjectIter and returns a *TreeIter that iterates over all
// tree contained in the storer.EncodedObjectIter.
//
// Any non-tree object returned by the storer.EncodedObjectIter is skipped.
func NewTreeIter(s storer.EncodedObjectStorer, iter storer.EncodedObjectIter) *TreeIter {
	return &TreeIter{iter, s}
}

// Next moves the iterator to the next tree and returns a pointer to it. If
// there are no more trees, it returns io.EOF.
func (iter *TreeIter) Next() (*Tree, error) {
	for {
		obj, err := iter.EncodedObjectIter.Next()
		if err != nil {
			return nil, err
		}

		if obj.Type() != plumbing.TreeObject {
			continue
		}

		return DecodeTree(iter.s, obj)
	}
}

// ForEach call the cb function for each tree contained on this iter until
// an error happens or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *TreeIter) ForEach(cb func(*Tree) error) error {
	return iter.EncodedObjectIter.ForEach(func(obj plumbing.EncodedObject) error {
		if obj.Type() != plumbing.TreeObject {
			return nil
		}

		t, err := DecodeTree(iter.s, obj)
		if err != nil {
			return err
		}

		return cb(t)
	})
}

func simpleJoin(parent, child string) string {
	if len(parent) > 0 {
		return parent + "/" + child
	}
	return child
}
