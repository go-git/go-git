package commitgraph

import (
	"errors"
	"math"

	"github.com/go-git/go-git/v5/plumbing"
	commitgraph "github.com/go-git/go-git/v5/plumbing/format/commitgraph/v2"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

type CreateCommitNodeGraphOptions struct {
	// Commits holds the commits to be iterated upon. If nil, then all commits in the storage are used as potential heads, otherwise only the commits in the iter are used.
	Commits object.CommitIter
	// Append defines whether all commits in the old index should be appended to the new index.
	Append bool
	// Chain defines whether the new index should be chained to the old index. Can only be used if Append is false and the old index has generation v2.
	Chain bool
}

// CreateCommitGraph takes a provided storage and possible old Index and creates a new commit graph.
// To create a pack specific graph pass in a storer that is restricted to the packfile.
func CreateCommitGraph(s storer.EncodedObjectStorer, old commitgraph.Index, opts CreateCommitNodeGraphOptions) (commitgraph.Index, error) {
	commitIndex := NewGraphCommitNodeIndex(old, s)

	var parent commitgraph.Index
	if opts.Chain && old != nil && old.HasGenerationV2() {
		parent = old
	}

	index := commitgraph.NewMemoryIndexWithParent(parent)
	heads := opts.Commits

	if heads == nil {
		objIter, err := s.IterEncodedObjects(plumbing.CommitObject)
		if err != nil {
			return nil, err
		}
		heads = object.NewCommitIter(s, objIter)
	}

	builder := &commitGraphBuilder{
		index:       index,
		toWalk:      make([]*object.Commit, 0, 32),
		commitIndex: commitIndex,
	}

	if opts.Append && old != nil {
		for i := uint32(0); i < old.MaximumNumberOfHashes(); i++ {
			hash, err := old.GetHashByIndex(i)
			if err != nil {
				return nil, err
			}
			commitNode, err := commitIndex.Get(hash)
			if err != nil {
				return nil, err
			}
			if err := builder.AddCommitNode(commitNode); err != nil {
				return nil, err
			}
		}
	}

	err := heads.ForEach(builder.AddCommit)
	if err != nil {
		return nil, err
	}
	builder.index.Sort()

	return builder.index, nil
}

var errParentsNotInGraph = errors.New("parents not in graph")

type commitGraphBuilder struct {
	index       *commitgraph.MemoryIndex
	toWalk      []*object.Commit // Stack of commits being walked - this may get as large as the longest chain
	commitIndex CommitNodeIndex
}

// AddCommitNode adds a commitnode read from a provided graph to the new graph
func (c *commitGraphBuilder) AddCommitNode(commitNode CommitNode) error {
	if c.has(commitNode.ID()) {
		return nil
	}
	if generationDataIsSet(commitNode) {
		// we can just copy this into the index
		c.copyCommitNode(commitNode.ID(), commitNode)
		return nil
	}

	// The provied commit node doesn't have valid generation data - treat it as a commit
	commit, err := commitNode.Commit()
	if err != nil {
		return err
	}
	return c.AddCommit(commit)
}

// AddCommit adds a commit and its parents to the new graph
func (c *commitGraphBuilder) AddCommit(commit *object.Commit) error {
	if c.has(commit.Hash) {
		return nil
	}

	c.push(commit)
	for {
		commit := c.peek()
		if commit == nil {
			break
		}
		if err := c.tryToAdd(commit); err == errParentsNotInGraph {
			continue
		} else if err != nil {
			return err
		}
		c.pop()
	}
	return nil
}

// peek returns the top of the stack of commits waiting to be added without removing it
func (c *commitGraphBuilder) peek() *object.Commit {
	if len(c.toWalk) == 0 {
		return nil
	}
	return c.toWalk[len(c.toWalk)-1]
}

// pop returns and removes the top of the stack of commits waiting to be added
func (c *commitGraphBuilder) pop() *object.Commit {
	if len(c.toWalk) == 0 {
		return nil
	}
	commit := c.toWalk[len(c.toWalk)-1]
	c.toWalk = c.toWalk[:len(c.toWalk)-1]
	if len(c.toWalk) == 0 && cap(c.toWalk) > 32 {
		// reset the stack to a small size to avoid keeping memory around
		c.toWalk = make([]*object.Commit, 0, 32)
	}
	return commit
}

// push adds a commit to the top of the stack of commits waiting to be added
func (c *commitGraphBuilder) push(commit *object.Commit) {
	c.toWalk = append(c.toWalk, commit)
}

// has returns true if the provided hash is already in the graph
func (c *commitGraphBuilder) has(hash plumbing.Hash) bool {
	_, err := c.index.GetIndexByHash(hash)
	return err == nil
}

// copyCommitNode copies a commitnode from the old graph to the new graph
func (c *commitGraphBuilder) copyCommitNode(hash plumbing.Hash, commitNode CommitNode) {
	c.index.Add(hash, &commitgraph.CommitData{
		TreeHash:     commitNode.TreeHash(),
		ParentHashes: commitNode.ParentHashes(),
		Generation:   commitNode.Generation(),
		GenerationV2: commitNode.GenerationV2(),
		When:         commitNode.CommitTime(),
	})
}

// generationDataIsSet returns true if the provided commitnode has valid generation data
func generationDataIsSet(commitNode CommitNode) bool {
	return commitNode.Generation() != math.MaxUint64 &&
		(commitNode.GenerationV2() != 0 || commitNode.CommitTime().Unix() == 0)
}

// tryToAdd will try to add a commit to the graph. If the parents of the commit are not already in
// the graph it will push them to the stack to be added and return errParentsNotInGraph.
func (c *commitGraphBuilder) tryToAdd(commit *object.Commit) error {
	if c.has(commit.Hash) {
		// Already in the graph and nothing to do
		return nil
	}

	// Get a commit node for this commit from the old index
	// - this will usually be a simple wrapper around the commit with invalid and unset generation data.
	// - but if the commit is in the old graph it will return valid generation data and we can add that directly
	commitNode, err := c.commitIndex.Get(commit.Hash)
	if err != nil {
		return err
	}
	if generationDataIsSet(commitNode) {
		// commitData can be copied in to the graph
		c.copyCommitNode(commit.Hash, commitNode)
		return nil
	}

	// If the generation data isn't set then the node is not in the graph yet
	// - therefore we should try to convert the commit into a valid commitdata
	// - this will return errParentsNotInGraph if the parents are not in the graph
	//   meaning we wiil need to walk this commit again later.
	commitData, err := c.convertToCommitData(commit, commitNode)
	if err != nil { // this includes errParentsNotInGraph
		return err
	}
	c.index.Add(commit.Hash, commitData)
	return nil
}

// convertToCommitData will convert a provided object.Commit and its wrapped graph CommitNode
// into a raw commitgraph CommitData to be added to the graph.
//
// Recall the generation of a Commit is the maximum generation of its parents + 1, and the
// generationV2 is the maximum of its commitTime as unix time, or its parents generationV2s + 1.
//
// Therefore we need the parents of this provided commit to be in the graph already.
// If they are not in the graph we need to add the parents to the stack and then walk them before
// we can add this commit to the graph.
func (c *commitGraphBuilder) convertToCommitData(commit *object.Commit, commitData CommitNode) (*commitgraph.CommitData, error) {
	// Default values for generation and generationV2
	generation, generationV2 := uint64(1), uint64(commitData.CommitTime().Unix())

	// Flag to label that all parents are in the graph
	parentsInGraph := true

	// Walk the parents of the commit
	err := commitData.ParentNodes().ForEach(func(parent CommitNode) error {
		// Get the index of the parent in the graph
		parentIdx, err := c.index.GetIndexByHash(parent.ID())
		if err != nil {
			// This parent is not in the graph yet...
			parentsInGraph = false

			// get the parent commit and push it to our stack to add next
			parentCommit, err := parent.Commit()
			if err != nil {
				return err
			}
			c.push(parentCommit)

			return nil
		}

		if !parentsInGraph { // no point loading the commit data from the index at this point
			return nil
		}

		// Load the data for this parent
		parentData, err := c.index.GetCommitDataByIndex(parentIdx)
		if err != nil {
			return err
		}

		// Update the generation and generationV2 if needed
		if generation <= parentData.Generation {
			generation = parentData.Generation + 1
		}
		if generationV2 <= parentData.GenerationV2 {
			generationV2 = parentData.GenerationV2 + 1
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// If all of our parents are not in the graph we need to abort and walk those
	// parents first.
	if !parentsInGraph {
		return nil, errParentsNotInGraph
	}

	// We can now create the commitData
	return &commitgraph.CommitData{
		TreeHash:     commitData.TreeHash(),
		ParentHashes: commitData.ParentHashes(),
		Generation:   generation,
		GenerationV2: generationV2,
		When:         commitData.CommitTime(),
	}, nil
}
