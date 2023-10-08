package commitgraph

import (
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"

	"github.com/emirpasic/gods/trees/binaryheap"
)

type commitNodeIteratorTopological struct {
	exploreStack commitNodeStackable
	visitStack   commitNodeStackable
	inCounts     map[plumbing.Hash]int

	ignore map[plumbing.Hash]struct{}
}

// NewCommitNodeIterTopoOrder returns a CommitNodeIter that walks the commit history,
// starting at the given commit and visiting its parents in a topological order but
// with the constraint that no parent is emitted before its children are emitted.
//
// This matches `git log --topo-order`
func NewCommitNodeIterTopoOrder(c CommitNode,
	seenExternal map[plumbing.Hash]bool,
	ignore []plumbing.Hash,
) CommitNodeIter {
	seen := composeIgnores(ignore, seenExternal)
	inCounts := make(map[plumbing.Hash]int)

	heap := &commitNodeHeap{binaryheap.NewWith(generationAndDateOrderComparator)}
	heap.Push(c)

	lifo := &commitNodeLifo{make([]CommitNode, 0, 8)}
	lifo.Push(c)

	return &commitNodeIteratorTopological{
		exploreStack: heap,
		visitStack:   lifo,
		inCounts:     inCounts,
		ignore:       seen,
	}
}

func (iter *commitNodeIteratorTopological) Next() (CommitNode, error) {
	var next CommitNode
	for {
		var ok bool
		next, ok = iter.visitStack.Pop()
		if !ok {
			return nil, io.EOF
		}

		if iter.inCounts[next.ID()] == 0 {
			break
		}
	}

	minimumLevel, generationV2 := next.GenerationV2(), true
	if minimumLevel == 0 {
		minimumLevel, generationV2 = next.Generation(), false
	}

	parents := make([]CommitNode, 0, len(next.ParentHashes()))
	for i := range next.ParentHashes() {
		pc, err := next.ParentNode(i)
		if err != nil {
			return nil, err
		}

		parents = append(parents, pc)

		if generationV2 {
			if pc.GenerationV2() < minimumLevel {
				minimumLevel = pc.GenerationV2()
			}
			continue
		}

		if pc.Generation() < minimumLevel {
			minimumLevel = pc.Generation()
		}
	}

	// EXPLORE
	for {
		toExplore, ok := iter.exploreStack.Peek()
		if !ok {
			break
		}

		if toExplore.ID() != next.ID() && iter.exploreStack.Size() == 1 {
			break
		}
		if generationV2 {
			if toExplore.GenerationV2() < minimumLevel {
				break
			}
		} else {
			if toExplore.Generation() < minimumLevel {
				break
			}
		}

		iter.exploreStack.Pop()
		for i, h := range toExplore.ParentHashes() {
			if _, has := iter.ignore[h]; has {
				continue
			}
			iter.inCounts[h]++

			if iter.inCounts[h] == 1 {
				pc, err := toExplore.ParentNode(i)
				if err != nil {
					return nil, err
				}
				iter.exploreStack.Push(pc)
			}
		}
	}

	// VISIT
	for i, h := range next.ParentHashes() {
		if _, has := iter.ignore[h]; has {
			continue
		}
		iter.inCounts[h]--

		if iter.inCounts[h] == 0 {
			iter.visitStack.Push(parents[i])
		}
	}
	delete(iter.inCounts, next.ID())

	return next, nil
}

func (iter *commitNodeIteratorTopological) ForEach(cb func(CommitNode) error) error {
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

func (iter *commitNodeIteratorTopological) Close() {
}
