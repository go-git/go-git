package index

import (
	"bytes"
	"path/filepath"

	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing/format/index"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/noder"
)

func IsEquals(a, b noder.Hasher) bool {
	pathA := a.(noder.Path)
	pathB := b.(noder.Path)
	if pathA[len(pathA)-1].IsDir() || pathB[len(pathB)-1].IsDir() {
		return false
	}

	return bytes.Equal(a.Hash(), b.Hash())
}

type Node struct {
	index  *index.Index
	parent string
	name   string
	entry  index.Entry
	isDir  bool
}

func NewRootNode(idx *index.Index) (*Node, error) {
	return &Node{index: idx, isDir: true}, nil
}

func (n *Node) String() string {
	return n.fullpath()
}

func (n *Node) Hash() []byte {
	if n.IsDir() {
		return nil
	}

	return append(n.entry.Hash[:], n.entry.Mode.Bytes()...)
}

func (n *Node) Name() string {
	return n.name
}

func (n *Node) IsDir() bool {
	return n.isDir
}

func (n *Node) Children() ([]noder.Noder, error) {
	path := n.fullpath()
	dirs := make(map[string]bool)

	var c []noder.Noder
	for _, e := range n.index.Entries {
		if e.Name == path {
			continue
		}

		prefix := path
		if prefix != "" {
			prefix += "/"
		}

		if !strings.HasPrefix(e.Name, prefix) {
			continue
		}

		name := e.Name[len(path):]
		if len(name) != 0 && name[0] == '/' {
			name = name[1:]
		}

		parts := strings.Split(name, "/")
		if len(parts) > 1 {
			dirs[parts[0]] = true
			continue
		}

		c = append(c, &Node{
			index:  n.index,
			parent: path,
			name:   name,
			entry:  e,
		})
	}

	for dir := range dirs {
		c = append(c, &Node{
			index:  n.index,
			parent: path,
			name:   dir,
			isDir:  true,
		})

	}

	return c, nil
}

func (n *Node) NumChildren() (int, error) {
	files, err := n.Children()
	return len(files), err
}

func (n *Node) fullpath() string {
	return filepath.Join(n.parent, n.name)
}
