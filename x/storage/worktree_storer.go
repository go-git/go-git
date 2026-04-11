package storage

import "github.com/go-git/go-billy/v6"

// WorktreeStorer provides access to the worktree filesystem.
type WorktreeStorer interface {
	Filesystem() billy.Filesystem
}
