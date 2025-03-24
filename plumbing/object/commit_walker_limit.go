package object

import (
	"errors"
	"io"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type commitLimitIter struct {
	sourceIter   CommitIter
	limitOptions LogLimitOptions
}

type LogLimitOptions struct {
	Since    *time.Time
	Until    *time.Time
	TailHash plumbing.Hash
}

func NewCommitLimitIterFromIter(commitIter CommitIter, limitOptions LogLimitOptions) CommitIter {
	iterator := new(commitLimitIter)
	iterator.sourceIter = commitIter
	iterator.limitOptions = limitOptions
	return iterator
}

func (c *commitLimitIter) Next() (*Commit, error) {
	for {
		commit, err := c.sourceIter.Next()
		if err != nil {
			return nil, err
		}

		if c.limitOptions.Since != nil && commit.Committer.When.Before(*c.limitOptions.Since) {
			continue
		}
		if c.limitOptions.Until != nil && commit.Committer.When.After(*c.limitOptions.Until) {
			continue
		}
		if c.limitOptions.TailHash == commit.Hash {
			return commit, storer.ErrStop
		}
		return commit, nil
	}
}

func (c *commitLimitIter) ForEach(cb func(*Commit) error) error {
	for {
		commit, nextErr := c.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil && !errors.Is(nextErr, storer.ErrStop) {
			return nextErr
		}
		err := cb(commit)
		if errors.Is(err, storer.ErrStop) || errors.Is(nextErr, storer.ErrStop) {
			return nil
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *commitLimitIter) Close() {
	c.sourceIter.Close()
}
