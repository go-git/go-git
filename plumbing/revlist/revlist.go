// Package revlist implements functions to walk the objects referenced by a
// commit history. Roughly equivalent to git-rev-list command.
package revlist

import (
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// Objects applies a complementary set. It gets all the hashes from all
// the reachable objects from the given commits. Ignore param are object hashes
// that we want to ignore on the result. It is a list because is
// easier to interact with other porcelain elements, but internally it is
// converted to a map. All that objects must be accessible from the object
// storer.
func Objects(
	s storer.EncodedObjectStorer,
	commits []*object.Commit,
	ignore []plumbing.Hash) ([]plumbing.Hash, error) {

	seen := hashListToSet(ignore)
	result := make(map[plumbing.Hash]bool)
	for _, c := range commits {
		err := reachableObjects(s, c, seen, func(h plumbing.Hash) error {
			if !seen[h] {
				result[h] = true
				seen[h] = true
			}

			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	return hashSetToList(result), nil
}

// reachableObjects returns, using the callback function, all the reachable
// objects from the specified commit. To avoid to iterate over seen commits,
// if a commit hash is into the 'seen' set, we will not iterate all his trees
// and blobs objects.
func reachableObjects(
	s storer.EncodedObjectStorer,
	commit *object.Commit,
	seen map[plumbing.Hash]bool,
	cb func(h plumbing.Hash) error) error {

	return iterateCommits(commit, func(commit *object.Commit) error {
		if seen[commit.Hash] {
			return nil
		}

		if err := cb(commit.Hash); err != nil {
			return err
		}

		return iterateCommitTrees(s, commit, func(h plumbing.Hash) error {
			return cb(h)
		})
	})
}

// iterateCommits iterate all reachable commits from the given one
func iterateCommits(commit *object.Commit, cb func(c *object.Commit) error) error {
	if err := cb(commit); err != nil {
		return err
	}

	return object.WalkCommitHistory(commit, func(c *object.Commit) error {
		return cb(c)
	})
}

// iterateCommitTrees iterate all reachable trees from the given commit
func iterateCommitTrees(
	s storer.EncodedObjectStorer,
	commit *object.Commit,
	cb func(h plumbing.Hash) error) error {

	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	if err := cb(tree.Hash); err != nil {
		return err
	}

	treeWalker := object.NewTreeWalker(tree, true)

	for {
		_, e, err := treeWalker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := cb(e.Hash); err != nil {
			return err
		}
	}

	return nil
}

func hashSetToList(hashes map[plumbing.Hash]bool) []plumbing.Hash {
	var result []plumbing.Hash
	for key := range hashes {
		result = append(result, key)
	}

	return result
}

func hashListToSet(hashes []plumbing.Hash) map[plumbing.Hash]bool {
	result := make(map[plumbing.Hash]bool)
	for _, h := range hashes {
		result[h] = true
	}

	return result
}
