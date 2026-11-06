package object

import (
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

// A treenoder is a helper type that wraps git trees into merkletrie
// noders.
//
// As a merkletrie noder doesn't understand the concept of modes (e.g.
// file permissions), the treenoder includes the mode of the git tree in
// the hash, so changes in the modes will be detected as modifications
// to the file contents by the merkletrie difftree algorithm.  This is
// consistent with how the "git diff-tree" command works.
type treeNoder struct {
	parent   *Tree  // the root node is its own parent
	name     string // empty string for the root node
	mode     filemode.FileMode
	hash     plumbing.Hash
	children []noder.Noder // memoized

	// prefix, when non-nil, scopes the node to the subtree at that path. Every
	// node the walk descends to carries the components of the prefix it stands
	// for, so a node of the chain is a directory of the wrapped tree while the
	// node the chain ends at is a real entry, with the hash the diff compares.
	prefix []string
}

// NewTreeRootNode returns the root node of a Tree
func NewTreeRootNode(t *Tree) noder.Noder {
	if t == nil {
		return &treeNoder{}
	}

	return &treeNoder{
		parent: t,
		name:   "",
		mode:   filemode.Dir,
		hash:   t.Hash,
	}
}

// NewTreeRootNodeWithPrefix returns the root node of a Tree scoped to the
// subtree at prefix, a slash-separated path. Walking it yields the entries the
// walk of the whole tree yields below the prefix, under the same paths. Only
// the prefix and its ancestors are descended into, so an unrelated sibling of
// the subtree is never entered and none of its tree objects is read.
//
// An empty prefix is the whole tree. A prefix the tree does not hold yields no
// entries, as the empty tree does; callers scope a diff with a directory they
// have already resolved, so an unheld prefix is a worktree that disagrees with
// its commit and contributes no staged changes.
func NewTreeRootNodeWithPrefix(t *Tree, prefix string) noder.Noder {
	if t == nil {
		return &treeNoder{}
	}

	components := splitPrefix(prefix)
	if components == nil {
		return NewTreeRootNode(t)
	}

	// A prefix can name a file or a gitlink, not only a directory: the index
	// and filesystem noders both keep the exact entry the prefix names, so a
	// directory-only check here would drop it and make the two trees disagree,
	// reporting an insertion and a deletion for an unchanged path.
	//
	// Resolve it by the same route Children uses. Tree only answers for a
	// directory, so a file prefix has to be looked up as an entry instead;
	// FindEntry is not usable here because it dereferences the cache map that a
	// freshly decoded tree has not built yet.
	joined := strings.Join(components, "/")
	if _, err := t.Tree(joined); err != nil {
		if _, err := treeEntryIn(t, components); err != nil {
			return &treeNoder{}
		}
	}

	return &treeNoder{
		parent: t,
		name:   "",
		mode:   filemode.Dir,
		hash:   t.Hash,
		prefix: components,
	}
}

// treeEntryIn returns the entry at the given components within t, which may be
// a file or a gitlink. It is the lookup Tree cannot do, because Tree only
// answers where the path names a directory.
func treeEntryIn(t *Tree, components []string) (*TreeEntry, error) {
	cur := t
	for i, name := range components {
		if i == len(components)-1 {
			return cur.entry(name)
		}

		next, err := cur.Tree(name)
		if err != nil {
			return nil, err
		}
		cur = next
	}

	return nil, ErrDirectoryNotFound
}

// splitPrefix returns the components of a slash-separated prefix, or nil when
// there is no prefix and the whole tree is wanted.
func splitPrefix(prefix string) []string {
	if strings.Trim(prefix, "/") == "" {
		return nil
	}

	return strings.Split(prefix, "/")
}

func (t *treeNoder) Skip() bool {
	return false
}

func (t *treeNoder) isRoot() bool {
	return t.name == ""
}

func (t *treeNoder) String() string {
	return "treeNoder <" + t.name + ">"
}

func (t *treeNoder) Hash() []byte {
	if t.mode == filemode.Deprecated {
		return append(t.hash.Bytes(), filemode.Regular.Bytes()...)
	}
	return append(t.hash.Bytes(), t.mode.Bytes()...)
}

func (t *treeNoder) Name() string {
	return t.name
}

func (t *treeNoder) IsDir() bool {
	return t.mode == filemode.Dir
}

// Children will return the children of a treenoder as treenoders,
// building them from the children of the wrapped git tree.
func (t *treeNoder) Children() ([]noder.Noder, error) {
	if t.mode != filemode.Dir {
		return noder.NoChildren, nil
	}

	// children are memoized for efficiency
	if t.children != nil {
		return t.children, nil
	}

	// A scoped walk descends along the prefix before it looks at anything else:
	// the node the prefix names is the only child of its parent that it wants,
	// and for the root, which is its own parent, the prefix chain has to be
	// followed before the ordinary listing below would enumerate the whole
	// tree. This branch therefore precedes the memoization-safe listing and is
	// what makes a scoped root yield its subtree rather than nothing.
	if len(t.prefix) > 0 {
		name := t.prefix[0]

		if len(t.prefix) == 1 {
			e, err := treeEntryIn(t.parent, t.prefix)
			if err != nil {
				return nil, err
			}

			t.children = []noder.Noder{&treeNoder{
				parent: t.parent,
				name:   name,
				mode:   e.Mode,
				hash:   e.Hash,
			}}
			return t.children, nil
		}

		child, err := t.parent.Tree(name)
		if err != nil {
			return nil, err
		}

		t.children = []noder.Noder{&treeNoder{
			parent: child,
			name:   name,
			mode:   filemode.Dir,
			prefix: t.prefix[1:],
		}}
		return t.children, nil
	}

	// the parent of the returned children will be ourself as a tree if
	// we are a not the root treenoder.  The root is special as it
	// is is own parent.
	parent := t.parent
	if !t.isRoot() {
		var err error
		if parent, err = t.parent.Tree(t.name); err != nil {
			return nil, err
		}
	}

	var err error
	t.children, err = transformChildren(parent)
	return t.children, err
}

// Returns the children of a tree as treenoders.
// Efficiency is key here.
func transformChildren(t *Tree) ([]noder.Noder, error) {
	var err error
	var e TreeEntry

	// there will be more tree entries than children in the tree,
	// due to submodules and empty directories, but I think it is still
	// worth it to pre-allocate the whole array now, even if sometimes
	// is bigger than needed.
	ret := make([]noder.Noder, 0, len(t.Entries))

	walker := NewTreeWalker(t, false, nil) // don't recurse
	// The diff walk is read-only and never materialises entry names into the
	// filesystem, so it must enumerate the tree faithfully — including entries
	// with names that are unsafe to check out but valid per upstream Git (e.g.
	// control characters). Path safety is enforced at materialisation
	// boundaries (FindEntry, TreeEntryFile, archive, FileIter), not here.
	walker.skipPathValidation = true
	// don't defer walker.Close() for efficiency reasons.
	for {
		_, e, err = walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			walker.Close()
			return nil, err
		}

		ret = append(ret, &treeNoder{
			parent: t,
			name:   e.Name,
			mode:   e.Mode,
			hash:   e.Hash,
		})
	}
	walker.Close()

	return ret, nil
}

// len(t.tree.Entries) != the number of elements walked by treewalker
// for some reason because of empty directories, submodules, etc, so we
// have to walk here.
func (t *treeNoder) NumChildren() (int, error) {
	children, err := t.Children()
	if err != nil {
		return 0, err
	}

	return len(children), nil
}
