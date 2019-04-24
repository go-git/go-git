package object

import (
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// objectCommitNode is a representation of Commit as presented in the GIT object format.
//
// objectCommitNode implements the CommitNode interface.
type objectCommitNode struct {
	nodeIndex CommitNodeIndex
	commit    *Commit
}

// NewObjectCommitNodeIndex returns CommitNodeIndex implementation that uses
// only object storage to load the nodes
func NewObjectCommitNodeIndex(s storer.EncodedObjectStorer) CommitNodeIndex {
	return &objectCommitNodeIndex{s}
}

func (oci *objectCommitNodeIndex) Get(hash plumbing.Hash) (CommitNode, error) {
	commit, err := GetCommit(oci.s, hash)
	if err != nil {
		return nil, err
	}

	return &objectCommitNode{
		nodeIndex: oci,
		commit:    commit,
	}, nil
}

// objectCommitNodeIndex is an index that can load CommitNode objects only from the
// object store.
//
// objectCommitNodeIndex implements the CommitNodeIndex interface
type objectCommitNodeIndex struct {
	s storer.EncodedObjectStorer
}

func (c *objectCommitNode) CommitTime() time.Time {
	return c.commit.Committer.When
}

func (c *objectCommitNode) ID() plumbing.Hash {
	return c.commit.ID()
}

func (c *objectCommitNode) Tree() (*Tree, error) {
	return c.commit.Tree()
}

func (c *objectCommitNode) NumParents() int {
	return c.commit.NumParents()
}

func (c *objectCommitNode) ParentNodes() CommitNodeIter {
	return newParentgraphCommitNodeIter(c)
}

func (c *objectCommitNode) ParentNode(i int) (CommitNode, error) {
	if i < 0 || i >= len(c.commit.ParentHashes) {
		return nil, ErrParentNotFound
	}

	// Note: It's necessary to go through CommitNodeIndex here to ensure
	// that if the commit-graph file covers only part of the history we
	// start using it when that part is reached.
	return c.nodeIndex.Get(c.commit.ParentHashes[i])
}

func (c *objectCommitNode) ParentHashes() []plumbing.Hash {
	return c.commit.ParentHashes
}

func (c *objectCommitNode) Commit() (*Commit, error) {
	return c.commit, nil
}
