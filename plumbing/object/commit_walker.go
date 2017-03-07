package object

import (
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
)

type commitWalker struct {
	seen  map[plumbing.Hash]bool
	stack []*CommitIter
	start *Commit
	cb    func(*Commit) error
}

// WalkCommitHistory walks the commit history, starting at the given commit and
// visiting its parents in pre-order. The given callback will be called for each
// visited commit. Each commit will be visited only once. If the callback returns
// an error, walking will stop and will return the error. Other errors might be
// returned if the history cannot be traversed (e.g. missing objects).
func WalkCommitHistory(c *Commit, cb func(*Commit) error) error {
	w := &commitWalker{
		seen:  make(map[plumbing.Hash]bool),
		stack: make([]*CommitIter, 0),
		start: c,
		cb:    cb,
	}

	return w.walk()
}

func (w *commitWalker) walk() error {
	var commit *Commit

	if w.start != nil {
		commit = w.start
		w.start = nil
	} else {
		current := len(w.stack) - 1
		if current < 0 {
			return nil
		}

		var err error
		commit, err = w.stack[current].Next()
		if err == io.EOF {
			w.stack = w.stack[:current]
			return w.walk()
		}

		if err != nil {
			return err
		}
	}

	// check and update seen
	if w.seen[commit.Hash] {
		return w.walk()
	}

	w.seen[commit.Hash] = true
	if commit.NumParents() > 0 {
		w.stack = append(w.stack, commit.Parents())
	}

	if err := w.cb(commit); err != nil {
		return err
	}

	return w.walk()
}
