package commitgraph

import (
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/emirpasic/gods/trees/binaryheap"
)

// NewCommitNodeIterAuthorDateOrder returns a CommitNodeIter that walks the commit history,
// starting at the given commit and visiting its parents in Author Time order but with the
// constraint that no parent is emitted before its children are emitted.
//
// This matches `git log --author-order`
//
// This ordering requires that commit objects need to be loaded into memory - thus this
// ordering is likely to be slower than other orderings.
func NewCommitNodeIterAuthorDateOrder(c CommitNode,
	seenExternal map[plumbing.Hash]bool,
	ignore []plumbing.Hash,
) CommitNodeIter {
	seen := make(map[plumbing.Hash]struct{})
	for _, h := range ignore {
		seen[h] = struct{}{}
	}
	for h, ext := range seenExternal {
		if ext {
			seen[h] = struct{}{}
		}
	}
	inCounts := make(map[plumbing.Hash]int)

	exploreHeap := &commitNodeHeap{binaryheap.NewWith(generationAndDateOrderComparator)}
	exploreHeap.Push(c)

	visitHeap := &commitNodeHeap{binaryheap.NewWith(func(left, right interface{}) int {
		leftCommit, err := left.(CommitNode).Commit()
		if err != nil {
			return -1
		}
		rightCommit, err := right.(CommitNode).Commit()
		if err != nil {
			return -1
		}

		switch {
		case rightCommit.Author.When.Before(leftCommit.Author.When):
			return -1
		case leftCommit.Author.When.Before(rightCommit.Author.When):
			return 1
		}
		return 0
	})}
	visitHeap.Push(c)

	return &commitNodeIteratorTopological{
		exploreStack: exploreHeap,
		visitStack:   visitHeap,
		inCounts:     inCounts,
		ignore:       seen,
	}
}
