package commitgraph

import (
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/emirpasic/gods/trees/binaryheap"
)

// NewCommitNodeIterDateOrder returns a CommitNodeIter that walks the commit history,
// starting at the given commit and visiting its parents in Committer Time and Generation order,
// but with the  constraint that no parent is emitted before its children are emitted.
//
// This matches `git log --date-order`
func NewCommitNodeIterDateOrder(c CommitNode,
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

	visitHeap := &commitNodeHeap{binaryheap.NewWith(generationAndDateOrderComparator)}
	visitHeap.Push(c)

	return &commitNodeIteratorTopological{
		exploreStack: exploreHeap,
		visitStack:   visitHeap,
		inCounts:     inCounts,
		ignore:       seen,
	}
}
