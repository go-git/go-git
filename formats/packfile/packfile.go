package packfile

import "fmt"

type Packfile struct {
	Version     uint32
	Size        int64
	ObjectCount int
	Checksum    []byte
	Commits     map[Hash]*Commit
	Trees       map[Hash]*Tree
	Blobs       map[Hash]*Blob
}

func NewPackfile() *Packfile {
	return &Packfile{
		Commits: make(map[Hash]*Commit, 0),
		Trees:   make(map[Hash]*Tree, 0),
		Blobs:   make(map[Hash]*Blob, 0),
	}
}

type BlobEntry struct {
	path string
	*Blob
}

type SubtreeEntry struct {
	path string
	*Tree
	TreeCh
}

type treeEntry interface {
	isTreeEntry()
	Path() string
}

func (b BlobEntry) isTreeEntry()    {}
func (b BlobEntry) Path() string    { return b.path }
func (b SubtreeEntry) isTreeEntry() {}
func (b SubtreeEntry) Path() string { return b.path }

type TreeCh <-chan treeEntry

func (p *Packfile) WalkCommit(commitHash Hash) (TreeCh, error) {
	commit, ok := p.Commits[commitHash]
	if !ok {
		return nil, fmt.Errorf("Unable to find %q commit", commitHash)
	}

	return p.WalkTree(p.Trees[commit.Tree]), nil
}

func (p *Packfile) WalkTree(tree *Tree) TreeCh {
	return p.walkTree(tree, "")
}

func (p *Packfile) walkTree(tree *Tree, pathPrefix string) TreeCh {
	ch := make(chan treeEntry)

	if tree == nil {
		close(ch)
		return ch
	}

	go func() {
		defer func() {
			close(ch)
		}()
		for _, e := range tree.Entries {
			path := pathPrefix + e.Name
			if blob, ok := p.Blobs[e.Hash]; ok {
				ch <- BlobEntry{path, blob}
			} else if subtree, ok := p.Trees[e.Hash]; ok {
				ch <- SubtreeEntry{path, subtree, p.walkTree(subtree, path+"/")}
			}
		}
	}()

	return ch
}
