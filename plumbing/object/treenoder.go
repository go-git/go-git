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
	path     string        // scope components still to follow below this node
}

// NewTreeRootNode returns the root node of a Tree
func NewTreeRootNode(t *Tree) noder.Noder {
	return NewTreeRootNodeWithOptions(t, TreeRootNodeOptions{})
}

// TreeRootNodeOptions contains configuration for a commit tree root node.
type TreeRootNodeOptions struct {
	// Path limits entries to this slash-separated, repository-relative file
	// or directory. An empty path includes all entries. Ancestor directory
	// nodes are retained so changes keep their repository-relative paths.
	Path string
}

// NewTreeRootNodeWithOptions returns the root node of a Tree with options.
func NewTreeRootNodeWithOptions(t *Tree, opts TreeRootNodeOptions) noder.Noder {
	if t == nil {
		return &treeNoder{}
	}

	return &treeNoder{
		parent: t,
		name:   "",
		mode:   filemode.Dir,
		hash:   t.Hash,
		path:   opts.Path,
	}
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

	if t.path != "" {
		// For scope "docs/generated", root.Children() returns only a docs
		// node, and docs.Children() returns only a generated node. The diff
		// constructs filenames from the names of the nodes it walks through:
		// root -> docs -> generated -> file.txt becomes docs/generated/file.txt.
		//
		// That full filename is also the key used for the file in the index.
		//
		// The constructed children have separate name and path fields:
		//   docs node:      name="docs",      path="generated"
		//   generated node: name="generated", path=""
		//
		// name contributes to the diff filename. path tells Children() which
		// child to select next. An empty path means no further filtering, so
		// generated.Children() calls transformChildren to return every entry
		// inside docs/generated, including any files and subdirectories.
		name, rest, _ := strings.Cut(t.path, "/")

		// Select the child from entries already loaded in the current tree.
		// transformChildren would read sibling directory objects through its
		// TreeWalker before we could discard those unrelated children.
		for _, entry := range parent.Entries {
			// Only the next scope component leads to the selected subtree,
			// sibling entries must not become children of this scoped node.
			if entry.Name != name {
				continue
			}
			// Remaining components require a directory to descend through.
			// A matching file or gitlink cannot contain the requested path,
			// so this tree contributes no entries within that scope.
			if rest != "" && entry.Mode != filemode.Dir {
				break
			}
			t.children = []noder.Noder{&treeNoder{
				parent: parent,
				name:   entry.Name,
				mode:   entry.Mode,
				hash:   entry.Hash,
				path:   rest,
			}}
			return t.children, nil
		}
		t.children = noder.NoChildren
		return t.children, nil
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
