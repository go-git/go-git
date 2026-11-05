// Package index provides a merkletrie noder implementation for git index entries.
package index

import (
	"path"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

// The node represents a index.Entry or a directory inferred from the path
// of all entries. It implements the interface noder.Noder of merkletrie
// package.
//
// This implementation implements a "standard" hash method being able to be
// compared with any other noder.Noder implementation inside of go-git
type node struct {
	path     string
	entry    *index.Entry
	children []noder.Noder
	isDir    bool
	skip     bool

	// treeHash is the tree a directory holds per the cache-tree, if hasTree.
	treeHash plumbing.Hash
	hasTree  bool

	upholdExecutableBit bool
}

// RootNodeOptions contains configuration for the root node.
type RootNodeOptions struct {
	UpholdExecutableBit bool

	// CacheTree hashes a directory as the tree recorded for it in the
	// index's cache-tree (idx.Cache, the TREE extension), when that entry is
	// valid and covers as many entries as the index holds below the
	// directory. A diff against a tree then skips a directory whose tree is
	// unchanged without reading it, as Git does through its cache-tree, so
	// that subtree may be absent from storage. Other directories hash to
	// zero, so a diff always descends into them.
	//
	// The cache-tree must describe idx.Entries. That holds for an index
	// decoded from a file; an index whose entries were changed since must
	// have its Cache cleared or updated.
	CacheTree bool
}

// NewRootNode returns the root node of a computed tree from a index.Index,
func NewRootNode(idx *index.Index) noder.Noder {
	return NewRootNodeWithOptions(idx, RootNodeOptions{UpholdExecutableBit: true})
}

// NewRootNodeWithOptions returns the root node of a computed tree from a index.Index,
func NewRootNodeWithOptions(idx *index.Index, options RootNodeOptions) noder.Noder {
	const rootNode = ""

	m := map[string]*node{rootNode: {isDir: true}}

	for _, e := range idx.Entries {
		parts := strings.Split(e.Name, string("/"))

		var fullpath string
		for _, part := range parts {
			parent := fullpath
			fullpath = path.Join(fullpath, part)

			// It's possible that the first occurrence of subdirectory is skipped.
			// The parent node can be created with SkipWorktree set to true, but
			// if any future children do not skip their subtree, the entire lineage
			// of the tree needs to have this value set to false so that subdirectories
			// are not ignored.
			if parentNode, ok := m[fullpath]; ok {
				if !e.SkipWorktree {
					parentNode.skip = false
				}
				continue
			}

			n := &node{path: fullpath, skip: e.SkipWorktree, upholdExecutableBit: options.UpholdExecutableBit}
			if fullpath == e.Name {
				n.entry = e
			} else {
				n.isDir = true
			}

			m[n.path] = n
			m[parent].children = append(m[parent].children, n)
		}
	}

	if options.CacheTree && idx.Cache != nil {
		applyCacheTree(m, idx)
	}

	return m[rootNode]
}

// applyCacheTree gives each directory in m the hash of its tree from idx's
// cache-tree, where that entry is valid and covers exactly the index
// entries below the directory, none of which is a conflict stage or an
// intent to add. Git leaves such an entry invalid, as no tree can be
// written from them.
//
// See all_trees_same_as_cache_tree and its use at
// https://github.com/git/git/blob/v2.54.0/unpack-trees.c#L754-L768 and
// https://github.com/git/git/blob/v2.54.0/unpack-trees.c#L880-L897, and
// https://github.com/git/git/blob/v2.54.0/cache-tree.c#L886-L897.
func applyCacheTree(m map[string]*node, idx *index.Index) {
	trees, ok := cacheTrees(idx.Cache.Entries, m)
	if !ok || len(trees) == 0 {
		return
	}

	type span struct {
		entries int
		untreed bool
	}
	spans := make(map[string]*span, len(trees))
	for dir := range trees {
		spans[dir] = &span{}
	}

	for _, e := range idx.Entries {
		untreed := e.Stage != 0 || e.IntentToAdd
		dir := e.Name
		for dir != "" {
			if i := strings.LastIndexByte(dir, '/'); i >= 0 {
				dir = dir[:i]
			} else {
				dir = ""
			}
			if s, ok := spans[dir]; ok {
				s.entries++
				s.untreed = s.untreed || untreed
			}
		}
	}

	for dir, t := range trees {
		n, ok := m[dir]
		s := spans[dir]
		if !ok || !n.isDir || s.untreed || s.entries != t.Entries {
			continue
		}
		n.treeHash = t.Hash
		n.hasTree = true
	}
}

// cacheTrees returns the valid entries of a cache-tree by directory path,
// for the directories of m. It reports false when the entries do not form
// a single tree in pre-order, so that none of them can be placed.
//
// The walk is iterative, and builds the path of an entry only while it
// names a directory of m: a crafted cache-tree nested far deeper than the
// index would otherwise cost time and memory quadratic in its depth.
func cacheTrees(entries []index.TreeEntry, m map[string]*node) (map[string]index.TreeEntry, bool) {
	if len(entries) == 0 || entries[0].Path != "" || entries[0].Trees < 0 {
		return nil, false
	}

	type frame struct {
		// dir is the directory's path, if it is one of m.
		dir   string
		inM   bool
		trees int
	}

	trees := make(map[string]index.TreeEntry)
	if entries[0].Entries > 0 {
		trees[""] = entries[0]
	}
	stack := []frame{{inM: true, trees: entries[0].Trees}}
	pos := 1
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.trees == 0 {
			stack = stack[:len(stack)-1]
			continue
		}
		top.trees--

		if pos >= len(entries) {
			return nil, false
		}
		e := entries[pos]
		pos++
		name := e.Path
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || e.Trees < 0 {
			return nil, false
		}

		child := frame{trees: e.Trees}
		if top.inM {
			dir := path.Join(top.dir, name)
			if n, ok := m[dir]; ok && n.isDir {
				child.dir, child.inM = dir, true
				if e.Entries > 0 {
					trees[dir] = e
				}
			}
		}
		stack = append(stack, child)
	}

	if pos != len(entries) {
		return nil, false
	}
	return trees, true
}

func (n *node) String() string {
	return n.path
}

func (n *node) Skip() bool {
	return n.skip
}

// Hash the hash of a filesystem is a 24-byte slice, is the result of
// concatenating the computed plumbing.Hash of the file as a Blob and its
// plumbing.FileMode; that way the difftree algorithm will detect changes in the
// contents of files and also in their mode.
//
// If the node is computed and not based on a index.Entry the hash is equals
// to a 24-bytes slices of zero values, unless RootNodeOptions.CacheTree gave
// it the hash of its tree, which is then followed by the directory mode, as
// a tree noder's is.
func (n *node) Hash() []byte {
	if n.entry == nil {
		if n.hasTree {
			return append(n.treeHash.Bytes(), filemode.Dir.Bytes()...)
		}
		return make([]byte, 24)
	}

	mode := n.entry.Mode
	if mode == filemode.Executable && !n.upholdExecutableBit {
		mode = filemode.Regular
	}

	return append(n.entry.Hash.Bytes(), mode.Bytes()...)
}

func (n *node) Name() string {
	return path.Base(n.path)
}

func (n *node) IsDir() bool {
	return n.isDir
}

func (n *node) Children() ([]noder.Noder, error) {
	return n.children, nil
}

func (n *node) NumChildren() (int, error) {
	return len(n.children), nil
}
