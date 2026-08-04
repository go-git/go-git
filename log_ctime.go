package git

import (
	"errors"
	"io"

	"github.com/emirpasic/gods/trees/binaryheap"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

type commitAllIteratorByCTime struct {
	store storer.EncodedObjectStorer
	seen  map[plumbing.Hash]bool
	heap  *binaryheap.Heap
}

func newCommitAllIterCTime(repoStorer storage.Storer) (object.CommitIter, error) {
	commits := make([]*object.Commit, 0)
	seen := make(map[plumbing.Hash]bool)
	addReference := func(ref *plumbing.Reference) {
		if seen[ref.Hash()] {
			return
		}

		commit, _ := object.GetCommit(repoStorer, ref.Hash())
		if commit == nil {
			return
		}

		seen[commit.Hash] = true
		commits = append(commits, commit)
	}

	head, err := storer.ResolveReference(repoStorer, plumbing.HEAD)
	if err == nil {
		addReference(head)
	}
	if err != nil && err != plumbing.ErrReferenceNotFound {
		return nil, err
	}

	refIter, err := repoStorer.IterReferences()
	if err != nil {
		return nil, err
	}
	defer refIter.Close()

	for {
		ref, err := refIter.Next()
		if err == io.EOF {
			break
		}
		if err == plumbing.ErrReferenceNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		addReference(ref)
	}

	heap := binaryheap.NewWith(func(a, b any) int {
		if a.(*object.Commit).Committer.When.Before(b.(*object.Commit).Committer.When) {
			return 1
		}
		return -1
	})
	for _, commit := range commits {
		heap.Push(commit)
	}

	return &commitAllIteratorByCTime{
		store: repoStorer,
		seen:  make(map[plumbing.Hash]bool),
		heap:  heap,
	}, nil
}

func (w *commitAllIteratorByCTime) Next() (*object.Commit, error) {
	for {
		commit, ok := w.heap.Pop()
		if !ok {
			return nil, io.EOF
		}
		current := commit.(*object.Commit)
		if w.seen[current.Hash] {
			continue
		}

		w.seen[current.Hash] = true
		for _, hash := range current.ParentHashes {
			if w.seen[hash] {
				continue
			}
			parent, err := object.GetCommit(w.store, hash)
			if err != nil {
				return nil, err
			}
			w.heap.Push(parent)
		}

		return current, nil
	}
}

func (w *commitAllIteratorByCTime) ForEach(cb func(*object.Commit) error) error {
	for {
		commit, err := w.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := cb(commit); err != nil {
			if errors.Is(err, storer.ErrStop) {
				return nil
			}
			return err
		}
	}
}

func (w *commitAllIteratorByCTime) Close() {}
