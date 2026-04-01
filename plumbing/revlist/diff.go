package revlist

import (
	"fmt"
	"sort"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ObjectsDiff computes the set of object hashes reachable from localCommits but not
// reachable from remoteCommits. It walks local and remote commits simultaneously
// in committer-time order, only advancing the remote side far enough to
// determine which local commits are new.
func ObjectsDiff(
	s storer.EncodedObjectStorer,
	localCommits []plumbing.Hash,
	remoteCommits []plumbing.Hash,
) ([]plumbing.Hash, error) {
	var localQueue []*object.Commit
	var remoteQueue []*object.Commit
	localSeen := make(map[plumbing.Hash]bool)
	remoteSeen := make(map[plumbing.Hash]bool)

	seen := make(map[plumbing.Hash]bool)
	var result []plumbing.Hash

	// insertSorted inserts a commit into a slice sorted by committer time
	// descending (newest first). For the small sizes involved in a typical
	// push this is faster than maintaining a heap.
	insertSorted := func(q *[]*object.Commit, c *object.Commit) {
		i := sort.Search(len(*q), func(i int) bool {
			return (*q)[i].Committer.When.Before(c.Committer.When)
		})
		*q = append(*q, nil)
		copy((*q)[i+1:], (*q)[i:])
		(*q)[i] = c
	}

	// Seed queues with tip commits.
	for _, h := range localCommits {
		c, err := object.GetCommit(s, h)
		if err != nil {
			return nil, fmt.Errorf("getting local commit %s: %w", h, err)
		}
		localSeen[h] = true
		insertSorted(&localQueue, c)
	}
	for _, h := range remoteCommits {
		c, err := object.GetCommit(s, h)
		if err != nil {
			continue
		}
		remoteSeen[h] = true
		insertSorted(&remoteQueue, c)
	}

	for len(localQueue) > 0 {
		// Pop whichever side has the newer commit.
		if len(remoteQueue) > 0 && !remoteQueue[0].Committer.When.Before(localQueue[0].Committer.When) {
			// Remote commit is newer or equal — pop it and mark as known.
			rc := remoteQueue[0]
			remoteQueue = remoteQueue[1:]
			for _, ph := range rc.ParentHashes {
				if remoteSeen[ph] {
					continue
				}
				remoteSeen[ph] = true
				if pc, err := object.GetCommit(s, ph); err == nil {
					insertSorted(&remoteQueue, pc)
				}
			}
			continue
		}

		// Local commit is newer — pop and process it.
		lc := localQueue[0]
		localQueue = localQueue[1:]

		if remoteSeen[lc.Hash] {
			// Boundary — remote already has this commit.
			continue
		}

		// New commit — collect its objects.
		if !seen[lc.Hash] {
			seen[lc.Hash] = true
			result = append(result, lc.Hash)
		}

		newTree, err := lc.Tree()
		if err != nil {
			return nil, fmt.Errorf("getting tree for %s: %w", lc.Hash, err)
		}

		var oldTree *object.Tree
		if lc.NumParents() > 0 {
			if parent, err := lc.Parent(0); err == nil {
				oldTree, _ = parent.Tree()
			}
		}

		if err := collectChangedTreeObjects(s, newTree, oldTree, seen, &result); err != nil {
			return nil, fmt.Errorf("diffing trees for %s: %w", lc.Hash, err)
		}

		// Insert parents into local queue.
		for _, ph := range lc.ParentHashes {
			if localSeen[ph] {
				continue
			}
			localSeen[ph] = true
			if pc, err := object.GetCommit(s, ph); err == nil {
				insertSorted(&localQueue, pc)
			}
		}
	}

	return result, nil
}

// collectChangedTreeObjects walks newTree, comparing entry hashes against
// oldTree. Subtrees with matching hashes are skipped entirely. Only new or
// modified tree and blob hashes are added to result.
func collectChangedTreeObjects(
	s storer.EncodedObjectStorer,
	newTree, oldTree *object.Tree,
	seen map[plumbing.Hash]bool,
	result *[]plumbing.Hash,
) error {
	if seen[newTree.Hash] {
		return nil
	}
	if oldTree != nil && newTree.Hash == oldTree.Hash {
		return nil
	}
	seen[newTree.Hash] = true
	*result = append(*result, newTree.Hash)

	// Index old tree entries by name for O(1) lookup.
	var oldEntries map[string]plumbing.Hash
	if oldTree != nil {
		oldEntries = make(map[string]plumbing.Hash, len(oldTree.Entries))
		for _, e := range oldTree.Entries {
			oldEntries[e.Name] = e.Hash
		}
	}

	for _, e := range newTree.Entries {
		if seen[e.Hash] {
			continue
		}
		if e.Mode == filemode.Submodule {
			continue
		}

		// If same name has same hash in old tree, unchanged—skip.
		if oldEntries != nil {
			if oldHash, ok := oldEntries[e.Name]; ok && oldHash == e.Hash {
				continue
			}
		}

		if e.Mode == filemode.Dir {
			// Recurse into changed subtree. The recursive call adds the
			// tree hash itself, so we don't add it here.
			newSub, err := object.GetTree(s, e.Hash)
			if err != nil {
				return fmt.Errorf("getting subtree %s: %w", e.Hash, err)
			}
			var oldSub *object.Tree
			if oldEntries != nil {
				if oldHash, ok := oldEntries[e.Name]; ok {
					oldSub, err = object.GetTree(s, oldHash)
					if err != nil {
						oldSub = nil
					}
				}
			}
			if err := collectChangedTreeObjects(s, newSub, oldSub, seen, result); err != nil {
				return err
			}
		} else {
			seen[e.Hash] = true
			*result = append(*result, e.Hash)
		}
	}

	return nil
}
