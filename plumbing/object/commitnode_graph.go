package object

import (
	"fmt"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// graphCommitNode is a reduced representation of Commit as presented in the commit
// graph file (commitgraph.Node). It is merely useful as an optimization for walking
// the commit graphs.
//
// graphCommitNode implements the CommitNode interface.
type graphCommitNode struct {
	// Hash for the Commit object
	hash plumbing.Hash
	// Index of the node in the commit graph file
	index int

	node *commitgraph.Node
	gci  *graphCommitNodeIndex
}

// graphCommitNodeIndex is an index that can load CommitNode objects from both the commit
// graph files and the object store.
//
// graphCommitNodeIndex implements the CommitNodeIndex interface
type graphCommitNodeIndex struct {
	commitGraph commitgraph.Index
	s           storer.EncodedObjectStorer
}

// NewGraphCommitNodeIndex returns CommitNodeIndex implementation that uses commit-graph
// files as backing storage and falls back to object storage when necessary
func NewGraphCommitNodeIndex(commitGraph commitgraph.Index, s storer.EncodedObjectStorer) CommitNodeIndex {
	return &graphCommitNodeIndex{commitGraph, s}
}

func (gci *graphCommitNodeIndex) Get(hash plumbing.Hash) (CommitNode, error) {
	// Check the commit graph first
	parentIndex, err := gci.commitGraph.GetIndexByHash(hash)
	if err == nil {
		parent, err := gci.commitGraph.GetNodeByIndex(parentIndex)
		if err != nil {
			return nil, err
		}

		return &graphCommitNode{
			hash:  hash,
			index: parentIndex,
			node:  parent,
			gci:   gci,
		}, nil
	}

	// Fallback to loading full commit object
	commit, err := GetCommit(gci.s, hash)
	if err != nil {
		return nil, err
	}

	return &objectCommitNode{
		nodeIndex: gci,
		commit:    commit,
	}, nil
}

func (c *graphCommitNode) ID() plumbing.Hash {
	return c.hash
}

func (c *graphCommitNode) Tree() (*Tree, error) {
	return GetTree(c.gci.s, c.node.TreeHash)
}

func (c *graphCommitNode) CommitTime() time.Time {
	return c.node.When
}

func (c *graphCommitNode) NumParents() int {
	return len(c.node.ParentIndexes)
}

func (c *graphCommitNode) ParentNodes() CommitNodeIter {
	return newParentgraphCommitNodeIter(c)
}

func (c *graphCommitNode) ParentNode(i int) (CommitNode, error) {
	if i < 0 || i >= len(c.node.ParentIndexes) {
		return nil, ErrParentNotFound
	}

	parent, err := c.gci.commitGraph.GetNodeByIndex(c.node.ParentIndexes[i])
	if err != nil {
		return nil, err
	}

	return &graphCommitNode{
		hash:  c.node.ParentHashes[i],
		index: c.node.ParentIndexes[i],
		node:  parent,
		gci:   c.gci,
	}, nil
}

func (c *graphCommitNode) ParentHashes() []plumbing.Hash {
	return c.node.ParentHashes
}

func (c *graphCommitNode) Commit() (*Commit, error) {
	return GetCommit(c.gci.s, c.hash)
}

func (c *graphCommitNode) String() string {
	return fmt.Sprintf(
		"%s %s\nDate:   %s",
		plumbing.CommitObject, c.ID(),
		c.CommitTime().Format(DateFormat),
	)
}
