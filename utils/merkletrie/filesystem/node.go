package filesystem

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/src-d/go-billy.v2"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/noder"
)

var ignore = map[string]bool{
	".git": true,
}

func IsEquals(a, b noder.Hasher) bool {
	pathA := a.(noder.Path)
	pathB := b.(noder.Path)
	if pathA[len(pathA)-1].IsDir() || pathB[len(pathB)-1].IsDir() {
		return false
	}

	return bytes.Equal(a.Hash(), b.Hash())
}

type Node struct {
	parent string
	name   string
	isDir  bool
	info   billy.FileInfo
	fs     billy.Filesystem
}

func NewRootNode(fs billy.Filesystem) (*Node, error) {
	info, err := fs.Stat("/")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return &Node{fs: fs, info: info, isDir: true, name: ""}, nil
}

func (n *Node) String() string {
	return filepath.Join(n.parent, n.name)
}

func (n *Node) Hash() []byte {
	if n.IsDir() {
		return nil
	}

	f, err := n.fs.Open(n.fullpath())
	if err != nil {
		panic(err)
	}

	h := plumbing.NewHasher(plumbing.BlobObject, n.info.Size())
	if _, err := io.Copy(h, f); err != nil {
		panic(err)
	}

	hash := h.Sum()
	mode, err := filemode.NewFromOSFileMode(n.info.Mode())
	if err != nil {
		panic(err)
	}

	return append(hash[:], mode.Bytes()...)
}

func (n *Node) Name() string {
	return n.name
}

func (n *Node) IsDir() bool {
	return n.isDir
}

func (n *Node) Children() ([]noder.Noder, error) {
	files, err := n.readDir()

	if err != nil {
		return nil, err
	}

	path := n.fullpath()
	var c []noder.Noder
	for _, file := range files {
		if _, ok := ignore[file.Name()]; ok {
			continue
		}

		c = append(c, &Node{
			fs:     n.fs,
			parent: path,
			info:   file,
			name:   file.Name(),
			isDir:  file.IsDir(),
		})
	}

	return c, nil
}

func (n *Node) NumChildren() (int, error) {
	files, err := n.readDir()
	return len(files), err
}

func (n *Node) fullpath() string {
	return filepath.Join(n.parent, n.name)
}

func (n *Node) readDir() ([]billy.FileInfo, error) {
	if !n.IsDir() {
		return nil, nil
	}

	l, err := n.fs.ReadDir(n.fullpath())
	if err != nil && os.IsNotExist(err) {
		return l, nil
	}

	return l, err
}
