package commitgraph

import (
	"errors"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"

	"github.com/emirpasic/gods/trees/binaryheap"
)

type commitNodeIteratorByCTime struct {
	heap         *binaryheap.Heap
	seenExternal map[plumbing.Hash]bool
	seen         map[plumbing.Hash]bool
}

// NewCommitNodeIterCTime returns a CommitNodeIter that walks the commit history,
// starting at the given commit and visiting its parents while preserving Committer Time order.
// this is close in order to `git log` but does not guarantee topological order and will
// order things incorrectly occasionally.
// The given callback will be called for each visited commit. Each commit will
// be visited only once. If the callback returns an error, walking will stop
// and will return the error. Other errors might be returned if the history
// cannot be traversed (e.g. missing objects). Ignore allows to skip some
// commits from being iterated.
func NewCommitNodeIterCTime(
	c CommitNode,
	seenExternal map[plumbing.Hash]bool,
	ignore []plumbing.Hash,
) CommitNodeIter {
	seen := make(map[plumbing.Hash]bool)
	for _, h := range ignore {
		seen[h] = true
	}

	heap := binaryheap.NewWith(func(a, b interface{}) int {
		if a.(CommitNode).CommitTime().Before(b.(CommitNode).CommitTime()) {
			return 1
		}
		return -1
	})

	heap.Push(c)

	return &commitNodeIteratorByCTime{
		heap:         heap,
		seenExternal: seenExternal,
		seen:         seen,
	}
}

func (w *commitNodeIteratorByCTime) Next() (CommitNode, error) {
	var c CommitNode
	for {
		cIn, ok := w.heap.Pop()
		if !ok {
			return nil, io.EOF
		}
		c = cIn.(CommitNode)
		cID := c.ID()

		if w.seen[cID] || w.seenExternal[cID] {
			continue
		}

		w.seen[cID] = true

		for i, h := range c.ParentHashes() {
			if w.seen[h] || w.seenExternal[h] {
				continue
			}
			pc, err := c.ParentNode(i)
			if err != nil {
				return nil, err
			}
			w.heap.Push(pc)
		}

		return c, nil
	}
}

func (w *commitNodeIteratorByCTime) ForEach(cb func(CommitNode) error) error {
	for {
		c, err := w.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		err = cb(c)
		if errors.Is(err, storer.ErrStop) {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (w *commitNodeIteratorByCTime) Close() {}
