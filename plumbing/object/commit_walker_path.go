package object

import (
	"errors"
	"io"
	"slices"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// NewCommitPathLogIter returns a CommitIter over the commits that
// `git log -- <path>` lists for the paths pathFilter accepts, simplifying the
// history as git does by default:
//
//   - a commit with one parent is listed when those paths differ between it
//     and its parent;
//   - a commit with no parent is listed when its tree holds one of them;
//   - a merge whose paths are the same as in one of its parents is not listed,
//     and the walk goes on through the first such parent alone;
//   - a merge whose paths differ from every parent is listed, and the walk
//     goes on through all of them.
//
// A commit a shallow clone cut the history at counts as a commit with no
// parent, as git counts it.
//
// commitIter decides the order the commits come in. It must be a walk made by
// NewCommitPreorderIter, NewCommitPostorderIter, NewCommitIterBSF or
// NewCommitIterCTime that has not yielded anything yet, so that it can be kept
// from the parents git does not follow; any other iterator is refused with an
// error.
//
// https://git-scm.com/docs/git-log#_history_simplification
func NewCommitPathLogIter(
	s storer.ShallowStorer,
	pathFilter func(string) bool,
	commitIter CommitIter,
) (CommitIter, error) {
	follower, ok := commitIter.(parentFollower)
	if !ok {
		return nil, errPathLogWalk
	}

	shallow, err := s.Shallow()
	if err != nil {
		return nil, err
	}

	history := &pathHistory{
		pathFilter: pathFilter,
		decisions:  make(map[plumbing.Hash]*pathDecision),
		shallow:    make(map[plumbing.Hash]bool, len(shallow)),
	}
	for _, hash := range shallow {
		history.shallow[hash] = true
	}

	follower.followParents(history.follow)
	return &commitPathLogIter{history: history, sourceIter: commitIter}, nil
}

// errPathLogWalk is returned when NewCommitPathLogIter is given an iterator it
// cannot keep from the parents git does not follow.
var errPathLogWalk = errors.New("path log needs a walk from NewCommitPreorderIter, NewCommitPostorderIter, NewCommitIterBSF or NewCommitIterCTime")

// pathHistory holds what git's history simplification decides about each
// commit of one history, for the paths one filter accepts.
type pathHistory struct {
	pathFilter func(string) bool
	decisions  map[plumbing.Hash]*pathDecision
	// shallow holds the commits a shallow clone cut the history at. Their
	// parents are not in the repository, and git judges them as commits with
	// no parent.
	shallow map[plumbing.Hash]bool
}

type pathDecision struct {
	// listed tells whether the log lists the commit.
	listed bool
	// parents are the parents the walk goes on to from the commit.
	parents []plumbing.Hash
}

// decide works out, once per commit, whether c is listed and which of its
// parents the walk goes on to.
func (h *pathHistory) decide(c *Commit) (*pathDecision, error) {
	if d, ok := h.decisions[c.Hash]; ok {
		return d, nil
	}

	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	parents := c.ParentHashes
	if h.shallow[c.Hash] {
		parents = nil
	}

	d := &pathDecision{listed: true, parents: parents}
	if len(parents) == 0 {
		same, err := h.sameAs(nil, tree)
		if err != nil {
			return nil, err
		}
		d.listed = !same
	}

	for _, hash := range parents {
		parent, err := GetCommit(c.s, hash)
		if err != nil {
			return nil, err
		}

		parentTree, err := parent.Tree()
		if err != nil {
			return nil, err
		}

		same, err := h.sameAs(parentTree, tree)
		if err != nil {
			return nil, err
		}

		if same {
			d.listed = false
			d.parents = []plumbing.Hash{hash}
			break
		}
	}

	h.decisions[c.Hash] = d
	return d, nil
}

// sameAs tells whether the paths the filter accepts are the same in both
// trees. A nil tree holds no path at all.
func (h *pathHistory) sameAs(from, to *Tree) (bool, error) {
	if from != nil && from.Hash == to.Hash {
		return true, nil
	}

	changes, err := DiffTree(from, to)
	if err != nil {
		return false, err
	}

	for _, change := range changes {
		if h.pathFilter(change.name()) {
			return false, nil
		}
	}

	return true, nil
}

// follow is the parentsFunc of the walk the log makes.
func (h *pathHistory) follow(c *Commit) ([]plumbing.Hash, error) {
	d, err := h.decide(c)
	if err != nil {
		return nil, err
	}

	return d.parents, nil
}

type commitPathLogIter struct {
	history    *pathHistory
	sourceIter CommitIter
}

func (c *commitPathLogIter) Next() (*Commit, error) {
	for {
		commit, err := c.sourceIter.Next()
		if err != nil {
			return nil, err
		}

		d, err := c.history.decide(commit)
		if err != nil {
			return nil, err
		}

		if d.listed {
			return commit, nil
		}
	}
}

func (c *commitPathLogIter) ForEach(cb func(*Commit) error) error {
	for {
		commit, err := c.Next()
		if errors.Is(err, io.EOF) {
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

func (c *commitPathLogIter) Close() {
	c.sourceIter.Close()
}

type commitPathIter struct {
	pathFilter    func(string) bool
	sourceIter    CommitIter
	currentCommit *Commit
	checkParent   bool
}

// NewCommitPathIterFromIter returns a commit iterator which performs diffTree between
// successive trees returned from the commit iterator from the argument. The purpose of this is
// to find the commits that explain how the files that match the path came to be.
// If checkParent is true then the function double checks if potential parent (next commit in a path)
// is one of the parents in the tree (it's used by `git log --all`).
// pathFilter is a function that takes path of file as argument and returns true if we want it
func NewCommitPathIterFromIter(pathFilter func(string) bool, commitIter CommitIter, checkParent bool) CommitIter {
	iterator := new(commitPathIter)
	iterator.sourceIter = commitIter
	iterator.pathFilter = pathFilter
	iterator.checkParent = checkParent
	return iterator
}

// NewCommitFileIterFromIter is kept for compatibility, can be replaced with NewCommitPathIterFromIter
func NewCommitFileIterFromIter(fileName string, commitIter CommitIter, checkParent bool) CommitIter {
	return NewCommitPathIterFromIter(
		func(path string) bool {
			return path == fileName
		},
		commitIter,
		checkParent,
	)
}

func (c *commitPathIter) Next() (*Commit, error) {
	if c.currentCommit == nil {
		var err error
		c.currentCommit, err = c.sourceIter.Next()
		if err != nil {
			return nil, err
		}
	}
	commit, commitErr := c.getNextFileCommit()

	// Setting current-commit to nil to prevent unwanted states when errors are raised
	if commitErr != nil {
		c.currentCommit = nil
	}
	return commit, commitErr
}

func (c *commitPathIter) getNextFileCommit() (*Commit, error) {
	var parentTree, currentTree *Tree

	for {
		// Parent-commit can be nil if the current-commit is the initial commit
		parentCommit, parentCommitErr := c.sourceIter.Next()
		if parentCommitErr != nil {
			// If the parent-commit is beyond the initial commit, keep it nil
			if parentCommitErr != io.EOF {
				return nil, parentCommitErr
			}
			parentCommit = nil
		}

		if parentTree == nil {
			var currTreeErr error
			currentTree, currTreeErr = c.currentCommit.Tree()
			if currTreeErr != nil {
				return nil, currTreeErr
			}
		} else {
			currentTree = parentTree
			parentTree = nil
		}

		if parentCommit != nil {
			var parentTreeErr error
			parentTree, parentTreeErr = parentCommit.Tree()
			if parentTreeErr != nil {
				return nil, parentTreeErr
			}
		}

		// Find diff between current and parent trees
		changes, diffErr := DiffTree(currentTree, parentTree)
		if diffErr != nil {
			return nil, diffErr
		}

		found := c.hasFileChange(changes, parentCommit)

		// Storing the current-commit in-case a change is found, and
		// Updating the current-commit for the next-iteration
		prevCommit := c.currentCommit
		c.currentCommit = parentCommit

		if found {
			return prevCommit, nil
		}

		// If not matches found and if parent-commit is beyond the initial commit, then return with EOF
		if parentCommit == nil {
			return nil, io.EOF
		}
	}
}

func (c *commitPathIter) hasFileChange(changes Changes, parent *Commit) bool {
	for _, change := range changes {
		if !c.pathFilter(change.name()) {
			continue
		}

		// filename matches, now check if source iterator contains all commits (from all refs)
		if c.checkParent {
			// Check if parent is beyond the initial commit
			if parent == nil || isParentHash(parent.Hash, c.currentCommit) {
				return true
			}
			continue
		}

		return true
	}

	return false
}

func isParentHash(hash plumbing.Hash, commit *Commit) bool {
	return slices.Contains(commit.ParentHashes, hash)
}

func (c *commitPathIter) ForEach(cb func(*Commit) error) error {
	for {
		commit, nextErr := c.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		err := cb(commit)
		if errors.Is(err, storer.ErrStop) {
			return nil
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *commitPathIter) Close() {
	c.sourceIter.Close()
}
