package git

import (
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
)

// RevListObjects applies a complementary set. It gets all the hashes from all
// the reachable objects from the given commits. Ignore param are object hashes
// that we want to ignore on the result. It is a list because is
// easier to interact with other porcelain elements, but internally it is
// converted to a map. All that objects must be accessible from the Repository.
func RevListObjects(
	r *Repository,
	commits []*Commit,
	ignore []plumbing.Hash) ([]plumbing.Hash, error) {

	seen := hashListToSet(ignore)
	result := make(map[plumbing.Hash]bool)
	for _, c := range commits {
		err := reachableObjects(r, c, seen, func(h plumbing.Hash) error {
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
	r *Repository,
	commit *Commit,
	seen map[plumbing.Hash]bool,
	cb func(h plumbing.Hash) error) error {

	return iterateCommits(commit, func(commit *Commit) error {
		if seen[commit.Hash] {
			return nil
		}

		if err := cb(commit.Hash); err != nil {
			return err
		}

		return iterateCommitTrees(r, commit, func(h plumbing.Hash) error {
			return cb(h)
		})
	})
}

// iterateCommits iterate all reachable commits from the given one
func iterateCommits(commit *Commit, cb func(c *Commit) error) error {
	if err := cb(commit); err != nil {
		return err
	}

	return WalkCommitHistory(commit, func(c *Commit) error {
		return cb(c)
	})
}

// iterateCommitTrees iterate all reachable trees from the given commit
func iterateCommitTrees(
	repository *Repository,
	commit *Commit,
	cb func(h plumbing.Hash) error) error {

	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	if err := cb(tree.Hash); err != nil {
		return err
	}

	treeWalker := NewTreeWalker(repository, tree, true)

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
