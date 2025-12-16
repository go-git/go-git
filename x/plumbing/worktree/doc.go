// Package worktree enables the management of multiple working trees attached
// to the same repository.
//
// A git repository can support multiple working trees, allowing you to check
// out more than one branch at a time.  With `git worktree add` a new working
// tree is associated with the repository, along with additional metadata
// that differentiates that working tree from others in the same repository.
// The working tree, along with this metadata, is called a "worktree".
//
// This new worktree is called a "linked worktree" as opposed to the "main
// worktree" prepared by linkgit:git-init[1] or linkgit:git-clone[1].
// A repository has one main worktree (if it's not a bare repository) and
// zero or more linked worktrees. When you are done with a linked worktree,
// remove it with `git worktree remove`.
//
// In its simplest form, `git worktree add <path>` automatically creates a
// new branch whose name is the final component of _<path>_, which is
// convenient if you plan to work on a new topic. For instance, `git
// worktree add ../hotfix` creates new branch `hotfix` and checks it out at
// path `../hotfix`. To instead work on an existing branch in a new worktree,
// use `git worktree add <path> <branch>`. On the other hand, if you just
// plan to make some experimental changes or do testing without disturbing
// existing development, it is often convenient to create a 'throwaway'
// worktree not associated with any branch. For instance,
// `git worktree add -d <path>` creates a new worktree with a detached `HEAD`
// at the same commit as the current branch.
//
// If a working tree is deleted without using `git worktree remove`, then
// its associated administrative files, which reside in the repository
// (see "DETAILS" below), will eventually be removed automatically (see
// `gc.worktreePruneExpire` in linkgit:git-config[1]), or you can run
// `git worktree prune` in the main or any linked worktree to clean up any
// stale administrative files.
//
// If the working tree for a linked worktree is stored on a portable device
// or network share which is not always mounted, you can prevent its
// administrative files from being pruned by issuing the `git worktree lock`
// command, optionally specifying `--reason` to explain why the worktree is
// locked.
//
// Ref: https://github.com/git/git/blob/master/Documentation/git-worktree.adoc
package worktree
