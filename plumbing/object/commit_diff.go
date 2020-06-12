package object

import (
	"io"

	"github.com/go-git/go-git/v5/plumbing/storer"
)

func DiffCommitIter(
	s storer.EncodedObjectStorer,
	start CommitIter,
	stop CommitIter,
	before func(a *Commit, b *Commit) bool,
) (CommitIter, error) {
	stopCommit, err := stop.Next()
	if err != nil {
		return nil, err
	}
	iter := &diffCommitIter{
		start:      start,
		stop:       stop,
		stopCommit: stopCommit,
		before:     before,
	}
	return iter, nil
}

type diffCommitIter struct {
	start      CommitIter
	stop       CommitIter
	stopCommit *Commit
	before     func(a *Commit, b *Commit) bool
	started    bool // set to true after first Next() call
}

func (iter *diffCommitIter) forwardStopCommit(commit *Commit) error {
	// forwards stop iter until before(stopCommit, commit) is false
	for iter.stopCommit != nil && iter.before(iter.stopCommit, commit) {
		var err error
		iter.stopCommit, err = iter.stop.Next()
		if err != nil {
			if err == io.EOF {
				// iter.stopCommit will be nil and iter.stop will be inactive
				// we may want to close iter.stop here and set it to nil
				return nil
			}
			return err
		}
	}
	return nil
}

func (iter *diffCommitIter) Next() (*Commit, error) {
	commit, err := iter.start.Next()
	if err != nil {
		return nil, err
	}
	if !iter.started {
		// this is an optimization, if first commit is already after stopCommit
		// then there is no commit in between them, iterator returns nothing
		// no need to load full history of start commit to know that
		iter.started = true
		if iter.stopCommit != nil && iter.before(iter.stopCommit, commit) {
			return nil, io.EOF
		}
	}
	// we have to maintain before(stopCommit, commit) to be false
	// meaning commit < stopCommit by the order defined by `before`
	// so when we more commit formard, we need to move stopCommit until
	// it passes commit or reaches the end of stop iter (then set stopCommit to nil)
	// if commit and stopCommit become the same, that's the end of iteration
	err = iter.forwardStopCommit(commit)
	if err != nil {
		return nil, err
	}
	if iter.stopCommit != nil && iter.stopCommit.Hash == commit.Hash {
		return nil, io.EOF
	}
	return commit, nil
}

func (iter *diffCommitIter) ForEach(cb func(*Commit) error) error {
	defer iter.Close()
	for {
		r, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := cb(r); err != nil {
			if err == storer.ErrStop {
				break
			}
			return err
		}
	}
	return nil
}

func (iter *diffCommitIter) Close() {
	if iter.start != nil {
		iter.start.Close()
	}
	if iter.stop != nil {
		iter.stop.Close()
	}
}
