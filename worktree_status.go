package git

import (
	"bytes"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/filesystem"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/index"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/noder"
)

// Status returns the working tree status
func (w *Worktree) Status() (Status, error) {
	ref, err := w.r.Head()
	if err == plumbing.ErrReferenceNotFound {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return w.status(ref.Hash())
}

func (w *Worktree) status(commit plumbing.Hash) (Status, error) {
	s := make(Status, 0)

	right, err := w.diffStagingWithWorktree()
	if err != nil {
		return nil, err
	}

	for _, ch := range right {
		a, err := ch.Action()
		if err != nil {
			return nil, err
		}

		switch a {
		case merkletrie.Delete:
			s.File(ch.From.String()).Worktree = Deleted
		case merkletrie.Insert:
			s.File(ch.To.String()).Worktree = Untracked
			s.File(ch.To.String()).Staging = Untracked
		case merkletrie.Modify:
			s.File(ch.To.String()).Worktree = Modified
		}
	}

	left, err := w.diffCommitWithStaging(commit, false)
	if err != nil {
		return nil, err
	}

	for _, ch := range left {
		a, err := ch.Action()
		if err != nil {
			return nil, err
		}

		switch a {
		case merkletrie.Delete:
			s.File(ch.From.String()).Staging = Deleted
		case merkletrie.Insert:
			s.File(ch.To.String()).Staging = Added
		case merkletrie.Modify:
			s.File(ch.To.String()).Staging = Modified
		}
	}

	return s, nil
}

func (w *Worktree) diffStagingWithWorktree() (merkletrie.Changes, error) {
	idx, err := w.r.Storer.Index()
	if err != nil {
		return nil, err
	}

	from := index.NewRootNode(idx)
	to := filesystem.NewRootNode(w.fs)
	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

func (w *Worktree) diffCommitWithStaging(commit plumbing.Hash, reverse bool) (merkletrie.Changes, error) {
	idx, err := w.r.Storer.Index()
	if err != nil {
		return nil, err
	}

	c, err := w.r.CommitObject(commit)
	if err != nil {
		return nil, err
	}

	t, err := c.Tree()
	if err != nil {
		return nil, err
	}

	to := index.NewRootNode(idx)
	from := object.NewTreeRootNode(t)

	if reverse {
		return merkletrie.DiffTree(to, from, diffTreeIsEquals)
	}

	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

var emptyNoderHash = make([]byte, 24)

// diffTreeIsEquals is a implementation of noder.Equals, used to compare
// noder.Noder, it compare the content and the length of the hashes.
//
// Since some of the noder.Noder implementations doesn't compute a hash for
// some directories, if any of the hashes is a 24-byte slice of zero values
// the comparison is not done and the hashes are take as different.
func diffTreeIsEquals(a, b noder.Hasher) bool {
	hashA := a.Hash()
	hashB := b.Hash()

	if bytes.Equal(hashA, emptyNoderHash) || bytes.Equal(hashB, emptyNoderHash) {
		return false
	}

	return bytes.Equal(hashA, hashB)
}
