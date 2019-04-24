package object

import (
	"fmt"
	"io"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// CommitNode is generic interface encapsulating either Commit object or
// graphCommitNode object
type CommitNode interface {
	ID() plumbing.Hash
	Tree() (*Tree, error)
	CommitTime() time.Time
	NumParents() int
	ParentNodes() CommitNodeIter
	ParentNode(i int) (CommitNode, error)
	ParentHashes() []plumbing.Hash
}

// CommitNodeIndex is generic interface encapsulating an index of CommitNode objects
// and accessor methods for walking it as a directed graph
type CommitNodeIndex interface {
	// Get returns a commit node from a commit hash
	Get(hash plumbing.Hash) (CommitNode, error)
	// Commit returns the full commit object from the node
	Commit(node CommitNode) (*Commit, error)
}

// CommitNodeIter is a generic closable interface for iterating over commit nodes.
type CommitNodeIter interface {
	Next() (CommitNode, error)
	ForEach(func(CommitNode) error) error
	Close()
}

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

// objectCommitNode is a representation of Commit as presented in the GIT object format.
//
// objectCommitNode implements the CommitNode interface.
type objectCommitNode struct {
	nodeIndex CommitNodeIndex
	commit    *Commit
}

// objectCommitNodeIndex is an index that can load CommitNode objects only from the
// object store.
//
// objectCommitNodeIndex implements the CommitNodeIndex interface
type objectCommitNodeIndex struct {
	s storer.EncodedObjectStorer
}

// ID returns the Commit object id referenced by the commit graph node.
func (c *graphCommitNode) ID() plumbing.Hash {
	return c.hash
}

// Tree returns the Tree referenced by the commit graph node.
func (c *graphCommitNode) Tree() (*Tree, error) {
	return GetTree(c.gci.s, c.node.TreeHash)
}

// CommitTime returns the Commiter.When time of the Commit referenced by the commit graph node.
func (c *graphCommitNode) CommitTime() time.Time {
	return c.node.When
}

// NumParents returns the number of parents in a commit.
func (c *graphCommitNode) NumParents() int {
	return len(c.node.ParentIndexes)
}

// ParentNodes return a CommitNodeIter for parents of specified node.
func (c *graphCommitNode) ParentNodes() CommitNodeIter {
	return newParentgraphCommitNodeIter(c)
}

// ParentNode returns the ith parent of a commit.
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

// ParentHashes returns hashes of the parent commits for a specified node
func (c *graphCommitNode) ParentHashes() []plumbing.Hash {
	return c.node.ParentHashes
}

func (c *graphCommitNode) String() string {
	return fmt.Sprintf(
		"%s %s\nDate:   %s",
		plumbing.CommitObject, c.ID(),
		c.CommitTime().Format(DateFormat),
	)
}

func NewGraphCommitNodeIndex(commitGraph commitgraph.Index, s storer.EncodedObjectStorer) CommitNodeIndex {
	return &graphCommitNodeIndex{commitGraph, s}
}

// NodeFromHash looks up a commit node by it's object hash
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

// Commit returns the full Commit object representing the commit graph node.
func (gci *graphCommitNodeIndex) Commit(node CommitNode) (*Commit, error) {
	if cgn, ok := node.(*graphCommitNode); ok {
		return GetCommit(gci.s, cgn.ID())
	}
	co := node.(*objectCommitNode)
	return co.commit, nil
}

// CommitTime returns the time when the commit was performed.
func (c *objectCommitNode) CommitTime() time.Time {
	return c.commit.Committer.When
}

// ID returns the Commit object id referenced by the node.
func (c *objectCommitNode) ID() plumbing.Hash {
	return c.commit.ID()
}

// Tree returns the Tree referenced by the node.
func (c *objectCommitNode) Tree() (*Tree, error) {
	return c.commit.Tree()
}

// NumParents returns the number of parents in a commit.
func (c *objectCommitNode) NumParents() int {
	return c.commit.NumParents()
}

// ParentNodes return a CommitNodeIter for parents of specified node.
func (c *objectCommitNode) ParentNodes() CommitNodeIter {
	return newParentgraphCommitNodeIter(c)
}

// ParentNode returns the ith parent of a commit.
func (c *objectCommitNode) ParentNode(i int) (CommitNode, error) {
	if i < 0 || i >= len(c.commit.ParentHashes) {
		return nil, ErrParentNotFound
	}

	return c.nodeIndex.Get(c.commit.ParentHashes[i])
}

// ParentHashes returns hashes of the parent commits for a specified node
func (c *objectCommitNode) ParentHashes() []plumbing.Hash {
	return c.commit.ParentHashes
}

func NewObjectCommitNodeIndex(s storer.EncodedObjectStorer) CommitNodeIndex {
	return &objectCommitNodeIndex{s}
}

// NodeFromHash looks up a commit node by it's object hash
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

// Commit returns the full Commit object representing the commit graph node.
func (oci *objectCommitNodeIndex) Commit(node CommitNode) (*Commit, error) {
	co := node.(*objectCommitNode)
	return co.commit, nil
}

// parentCommitNodeIter provides an iterator for parent commits from associated CommitNodeIndex.
type parentCommitNodeIter struct {
	node CommitNode
	i    int
}

func newParentgraphCommitNodeIter(node CommitNode) CommitNodeIter {
	return &parentCommitNodeIter{node, 0}
}

// Next moves the iterator to the next commit and returns a pointer to it. If
// there are no more commits, it returns io.EOF.
func (iter *parentCommitNodeIter) Next() (CommitNode, error) {
	obj, err := iter.node.ParentNode(iter.i)
	if err == ErrParentNotFound {
		return nil, io.EOF
	}
	if err == nil {
		iter.i++
	}

	return obj, err
}

// ForEach call the cb function for each commit contained on this iter until
// an error appends or the end of the iter is reached. If ErrStop is sent
// the iteration is stopped but no error is returned. The iterator is closed.
func (iter *parentCommitNodeIter) ForEach(cb func(CommitNode) error) error {
	for {
		obj, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if err := cb(obj); err != nil {
			if err == storer.ErrStop {
				return nil
			}

			return err
		}
	}
}

func (iter *parentCommitNodeIter) Close() {
}
