package worktree

import (
	"errors"

	"github.com/go-git/go-git/v6/plumbing"
)

type options struct {
	commit       plumbing.Hash
	detachedHead bool
}

func (o *options) Validate() error {
	if o.commit.IsZero() {
		return errors.New("commit is nil")
	}
	return nil
}

// Option is a functional option for configuring worktree operations.
// Options are passed to methods like Add to customize their behavior.
type Option func(*options)

// WithCommit specifies the commit hash to check out when adding a new worktree.
//
// The specified commit will be checked out in the new worktree, and both HEAD
// and ORIG_HEAD will be set to point to this commit.
func WithCommit(commit plumbing.Hash) Option {
	return func(o *options) {
		o.commit = commit
	}
}

// WithDetachedHead creates the worktree with a detached HEAD at the specified commit.
//
// Use this option to create a detached HEAD instead, similar to `git worktree add --detach <path>`.
func WithDetachedHead() Option {
	return func(o *options) {
		o.detachedHead = true
	}
}
