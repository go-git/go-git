// Package worktree manages linked worktrees: additional working trees that
// are attached to one repository, each with its own HEAD and index.
//
// A [Worktree] is created from the storer of the main repository. It can
// add a linked worktree on a filesystem, list the ones the repository
// knows, open one as a repository, and remove its metadata again.
//
// The concepts follow git-worktree(1):
// https://git-scm.com/docs/git-worktree
package worktree
