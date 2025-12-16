package worktree

import (
	"errors"

	"github.com/go-git/go-git/v6/plumbing"
)

type options struct {
	commit plumbing.Hash
}

func (o *options) Validate() error {
	if o.commit.IsZero() {
		return errors.New("commit ID cannot be zero")
	}

	return nil
}

type Option func(*options)

func WithCommit(commit plumbing.Hash) Option {
	return func(o *options) {
		o.commit = commit
	}
}
