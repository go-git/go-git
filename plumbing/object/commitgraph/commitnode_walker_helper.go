package commitgraph

import (
	"math"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/emirpasic/gods/trees/binaryheap"
)

// commitNodeStackable represents a common interface between heaps and stacks
type commitNodeStackable interface {
	Push(c CommitNode)
	Pop() (CommitNode, bool)
	Peek() (CommitNode, bool)
	Size() int
}

// commitNodeLifo is a stack implementation using an underlying slice
type commitNodeLifo struct {
	l []CommitNode
}

// Push pushes a new CommitNode to the stack
func (l *commitNodeLifo) Push(c CommitNode) {
	l.l = append(l.l, c)
}

// Pop pops the most recently added CommitNode from the stack
func (l *commitNodeLifo) Pop() (CommitNode, bool) {
	if len(l.l) == 0 {
		return nil, false
	}
	c := l.l[len(l.l)-1]
	l.l = l.l[:len(l.l)-1]
	return c, true
}

// Peek returns the most recently added CommitNode from the stack without removing it
func (l *commitNodeLifo) Peek() (CommitNode, bool) {
	if len(l.l) == 0 {
		return nil, false
	}
	return l.l[len(l.l)-1], true
}

// Size returns the number of CommitNodes in the stack
func (l *commitNodeLifo) Size() int {
	return len(l.l)
}

// commitNodeHeap is a stack implementation using an underlying binary heap
type commitNodeHeap struct {
	*binaryheap.Heap
}

// Push pushes a new CommitNode to the heap
func (h *commitNodeHeap) Push(c CommitNode) {
	h.Heap.Push(c)
}

// Pop removes top element on heap and returns it, or nil if heap is empty.
// Second return parameter is true, unless the heap was empty and there was nothing to pop.
func (h *commitNodeHeap) Pop() (CommitNode, bool) {
	c, ok := h.Heap.Pop()
	if !ok {
		return nil, false
	}
	return c.(CommitNode), true
}

// Peek returns top element on the heap without removing it, or nil if heap is empty.
// Second return parameter is true, unless the heap was empty and there was nothing to peek.
func (h *commitNodeHeap) Peek() (CommitNode, bool) {
	c, ok := h.Heap.Peek()
	if !ok {
		return nil, false
	}
	return c.(CommitNode), true
}

// Size returns number of elements within the heap.
func (h *commitNodeHeap) Size() int {
	return h.Heap.Size()
}

// generationAndDateOrderComparator compares two CommitNode objects based on their generation and commit time.
// If the left CommitNode object is in a higher generation or is newer than the right one, it returns a -1.
// If the left CommitNode object is in a lower generation or is older than the right one, it returns a 1.
// If the two CommitNode objects have the same commit time and generation, it returns 0.
func generationAndDateOrderComparator(left, right interface{}) int {
	leftCommit := left.(CommitNode)
	rightCommit := right.(CommitNode)

	// if GenerationV2 is MaxUint64, then the node is not in the graph
	if leftCommit.GenerationV2() == math.MaxUint64 {
		if rightCommit.GenerationV2() == math.MaxUint64 {
			switch {
			case rightCommit.CommitTime().Before(leftCommit.CommitTime()):
				return -1
			case leftCommit.CommitTime().Before(rightCommit.CommitTime()):
				return 1
			}
			return 0
		}
		// left is not in the graph, but right is, so it is newer than the right
		return -1
	}

	if rightCommit.GenerationV2() == math.MaxInt64 {
		// the right is not in the graph, therefore the left is before the right
		return 1
	}

	if leftCommit.GenerationV2() == 0 || rightCommit.GenerationV2() == 0 {
		// We need to assess generation and date
		if leftCommit.Generation() < rightCommit.Generation() {
			return 1
		}
		if leftCommit.Generation() > rightCommit.Generation() {
			return -1
		}
		switch {
		case rightCommit.CommitTime().Before(leftCommit.CommitTime()):
			return -1
		case leftCommit.CommitTime().Before(rightCommit.CommitTime()):
			return 1
		}
		return 0
	}

	if leftCommit.GenerationV2() < rightCommit.GenerationV2() {
		return 1
	}
	if leftCommit.GenerationV2() > rightCommit.GenerationV2() {
		return -1
	}

	return 0
}

// composeIgnores composes the ignore list with the provided seenExternal list
func composeIgnores(ignore []plumbing.Hash, seenExternal map[plumbing.Hash]bool) map[plumbing.Hash]struct{} {
	if len(ignore) == 0 {
		seen := make(map[plumbing.Hash]struct{})
		for h, ext := range seenExternal {
			if ext {
				seen[h] = struct{}{}
			}
		}
		return seen
	}

	seen := make(map[plumbing.Hash]struct{})
	for _, h := range ignore {
		seen[h] = struct{}{}
	}
	for h, ext := range seenExternal {
		if ext {
			seen[h] = struct{}{}
		}
	}
	return seen
}
