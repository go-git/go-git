package revlist

import (
	"errors"
	"fmt"
	"sort"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// objectWalk holds the state for a single Objects computation.
type objectWalk struct {
	s          storer.EncodedObjectStorer
	wantsQueue []*object.Commit
	havesQueue []*object.Commit
	wantsSeen  map[plumbing.Hash]struct{}
	havesSeen  map[plumbing.Hash]struct{}
	seen       map[plumbing.Hash]struct{}
	result     []plumbing.Hash
}

func newObjectWalk(s storer.EncodedObjectStorer) *objectWalk {
	return &objectWalk{
		s:         s,
		wantsSeen: make(map[plumbing.Hash]struct{}),
		havesSeen: make(map[plumbing.Hash]struct{}),
		seen:      make(map[plumbing.Hash]struct{}),
	}
}

// seedWants resolves each want hash and enqueues commits for walking.
// Non-commit objects (blobs, trees, tags) are added directly to the result.
func (w *objectWalk) seedWants(wants []plumbing.Hash) error {
	for i := 0; i < len(wants); i++ {
		h := wants[i]
		if _, ok := w.wantsSeen[h]; ok {
			continue
		}
		if _, ok := w.seen[h]; ok {
			continue
		}

		o, err := w.s.EncodedObject(plumbing.AnyObject, h)
		if err != nil {
			return fmt.Errorf("getting wanted object %s: %w", h, err)
		}

		switch o.Type() {
		case plumbing.CommitObject:
			c, err := object.DecodeCommit(w.s, o)
			if err != nil {
				return fmt.Errorf("decoding commit %s: %w", h, err)
			}
			w.wantsSeen[h] = struct{}{}
			insertSorted(&w.wantsQueue, c)
		case plumbing.TagObject:
			tag, err := object.DecodeTag(w.s, o)
			if err != nil {
				return fmt.Errorf("decoding tag %s: %w", h, err)
			}
			w.seen[tag.Hash] = struct{}{}
			w.result = append(w.result, tag.Hash)
			wants = append(wants, tag.Target)
		case plumbing.TreeObject:
			t, err := object.GetTree(w.s, h)
			if err != nil {
				return fmt.Errorf("getting tree %s: %w", h, err)
			}
			if err := collectAllTreeObjects(w.s, t, w.seen, &w.result); err != nil {
				return err
			}
		case plumbing.BlobObject:
			w.seen[h] = struct{}{}
			w.result = append(w.result, h)
		default:
			return fmt.Errorf("unsupported object type %s for %s", o.Type(), h)
		}
	}
	return nil
}

// seedHaves enqueues each have commit and pre-populates seen with all
// tree/blob objects reachable from the haves tips. Non-commit objects
// (tags, trees, blobs) are marked as seen so the diff walk skips them.
// Missing objects (ErrObjectNotFound) are tolerated since the remote
// may advertise refs we don't have locally.
func (w *objectWalk) seedHaves(haves []plumbing.Hash) error {
	for i := 0; i < len(haves); i++ {
		h := haves[i]
		if _, ok := w.havesSeen[h]; ok {
			continue
		}
		if _, ok := w.seen[h]; ok {
			continue
		}

		o, err := w.s.EncodedObject(plumbing.AnyObject, h)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				continue
			}
			return fmt.Errorf("getting haves object %s: %w", h, err)
		}

		switch o.Type() {
		case plumbing.CommitObject:
			c, err := object.DecodeCommit(w.s, o)
			if err != nil {
				return fmt.Errorf("decoding haves commit %s: %w", h, err)
			}
			w.havesSeen[h] = struct{}{}
			insertSorted(&w.havesQueue, c)
			if t, err := c.Tree(); err == nil {
				markTreeSeen(w.s, t, w.seen)
			}
		case plumbing.TagObject:
			tag, err := object.DecodeTag(w.s, o)
			if err != nil {
				return fmt.Errorf("decoding haves tag %s: %w", h, err)
			}
			w.seen[tag.Hash] = struct{}{}
			haves = append(haves, tag.Target)
		case plumbing.TreeObject:
			if t, err := object.GetTree(w.s, h); err == nil {
				markTreeSeen(w.s, t, w.seen)
			}
		case plumbing.BlobObject:
			w.seen[h] = struct{}{}
		}
	}
	return nil
}

// walk runs the interleaved commit walk, collecting new objects.
func (w *objectWalk) walk() error {
	// Fast path: no haves means we need all reachable objects.
	// A simple seen-set tree walk is much cheaper than per-commit diffs.
	if len(w.havesQueue) == 0 {
		return w.walkFull()
	}
	for len(w.wantsQueue) > 0 {
		// Pop whichever side has the newer commit.
		if len(w.havesQueue) > 0 && !w.havesQueue[0].Committer.When.Before(w.wantsQueue[0].Committer.When) {
			w.advanceHaves()
			continue
		}

		if err := w.processWant(); err != nil {
			return err
		}
	}
	return nil
}

// walkFull is the fast path when there are no haves. It walks all
// commits and collects every reachable tree/blob via a simple seen-set
// traversal — no per-commit tree diffs needed.
func (w *objectWalk) walkFull() error {
	for len(w.wantsQueue) > 0 {
		lc := w.wantsQueue[0]
		w.wantsQueue = w.wantsQueue[1:]

		if _, ok := w.seen[lc.Hash]; ok {
			continue
		}
		w.seen[lc.Hash] = struct{}{}
		w.result = append(w.result, lc.Hash)

		tree, err := lc.Tree()
		if err != nil {
			return fmt.Errorf("getting tree for %s: %w", lc.Hash, err)
		}

		if err := collectAllTreeObjects(w.s, tree, w.seen, &w.result); err != nil {
			return fmt.Errorf("collecting tree objects for %s: %w", lc.Hash, err)
		}

		for _, ph := range lc.ParentHashes {
			if _, ok := w.wantsSeen[ph]; ok {
				continue
			}
			w.wantsSeen[ph] = struct{}{}
			pc, err := object.GetCommit(w.s, ph)
			if err != nil {
				return fmt.Errorf("getting parent commit %s: %w", ph, err)
			}
			insertSorted(&w.wantsQueue, pc)
		}
	}
	return nil
}

// advanceHaves pops the newest haves commit and enqueues its parents.
func (w *objectWalk) advanceHaves() {
	rc := w.havesQueue[0]
	w.havesQueue = w.havesQueue[1:]
	for _, ph := range rc.ParentHashes {
		if _, ok := w.havesSeen[ph]; ok {
			continue
		}
		w.havesSeen[ph] = struct{}{}
		if pc, err := object.GetCommit(w.s, ph); err == nil {
			insertSorted(&w.havesQueue, pc)
		}
	}
}

// processWant pops the newest wants commit, collects its new objects,
// and enqueues its parents.
func (w *objectWalk) processWant() error {
	lc := w.wantsQueue[0]
	w.wantsQueue = w.wantsQueue[1:]

	if _, ok := w.havesSeen[lc.Hash]; ok {
		return nil // boundary — haves already has this commit
	}

	// New commit — collect its objects.
	if _, ok := w.seen[lc.Hash]; !ok {
		w.seen[lc.Hash] = struct{}{}
		w.result = append(w.result, lc.Hash)
	}

	newTree, err := lc.Tree()
	if err != nil {
		return fmt.Errorf("getting tree for %s: %w", lc.Hash, err)
	}

	var oldTrees []*object.Tree
	for i := 0; i < lc.NumParents(); i++ {
		if parent, err := lc.Parent(i); err == nil {
			if pt, err := parent.Tree(); err == nil {
				oldTrees = append(oldTrees, pt)
			}
		}
	}

	if err := collectChangedTreeObjects(w.s, newTree, oldTrees, w.seen, &w.result); err != nil {
		return fmt.Errorf("diffing trees for %s: %w", lc.Hash, err)
	}

	// Enqueue parents.
	for _, ph := range lc.ParentHashes {
		if _, ok := w.wantsSeen[ph]; ok {
			continue
		}
		w.wantsSeen[ph] = struct{}{}
		pc, err := object.GetCommit(w.s, ph)
		if err != nil {
			return fmt.Errorf("getting parent commit %s: %w", ph, err)
		}
		insertSorted(&w.wantsQueue, pc)
	}
	return nil
}

// insertSorted inserts a commit into a slice sorted by committer time
// descending (newest first).
func insertSorted(q *[]*object.Commit, c *object.Commit) {
	i := sort.Search(len(*q), func(i int) bool {
		return (*q)[i].Committer.When.Before(c.Committer.When)
	})
	*q = append(*q, nil)
	copy((*q)[i+1:], (*q)[i:])
	(*q)[i] = c
}

// collectChangedTreeObjects walks newTree, comparing entry hashes against
// all oldTrees. An entry is considered unchanged if any old tree contains the
// same name with the same hash. Only new or modified tree and blob hashes are
// added to result.
func collectChangedTreeObjects(
	s storer.EncodedObjectStorer,
	newTree *object.Tree,
	oldTrees []*object.Tree,
	seen map[plumbing.Hash]struct{},
	result *[]plumbing.Hash,
) error {
	// If newTree matches any old tree exactly, nothing changed.
	for _, ot := range oldTrees {
		if newTree.Hash == ot.Hash {
			return nil
		}
	}

	if _, ok := seen[newTree.Hash]; !ok {
		seen[newTree.Hash] = struct{}{}
		*result = append(*result, newTree.Hash)
	}

	// Build per-parent entry indexes for O(1) lookup.
	oldEntryMaps := make([]map[string]plumbing.Hash, len(oldTrees))
	for i, ot := range oldTrees {
		oldEntryMaps[i] = make(map[string]plumbing.Hash, len(ot.Entries))
		for _, e := range ot.Entries {
			oldEntryMaps[i][e.Name] = e.Hash
		}
	}

	for _, e := range newTree.Entries {
		// Skip blobs we've already collected. Directories are not skipped
		// here — a tree hash being "seen" means the hash itself was added
		// to the result, but a prior diff-walk may not have collected all
		// of its children. The recursive call handles dedup for trees.
		if e.Mode != filemode.Dir {
			if _, ok := seen[e.Hash]; ok {
				continue
			}
		}
		if e.Mode == filemode.Submodule {
			continue
		}

		// If same name has same hash in any parent tree, unchanged—skip.
		unchanged := false
		for _, m := range oldEntryMaps {
			if oh, ok := m[e.Name]; ok && oh == e.Hash {
				unchanged = true
				break
			}
		}
		if unchanged {
			continue
		}

		if e.Mode == filemode.Dir {
			// Recurse into changed subtree. Collect the old versions of
			// this subtree from all parents that have it.
			newSub, err := object.GetTree(s, e.Hash)
			if err != nil {
				return fmt.Errorf("getting subtree %s: %w", e.Hash, err)
			}
			var oldSubs []*object.Tree
			for _, m := range oldEntryMaps {
				if oh, ok := m[e.Name]; ok {
					if ot, err := object.GetTree(s, oh); err == nil {
						oldSubs = append(oldSubs, ot)
					}
				}
			}
			if err := collectChangedTreeObjects(s, newSub, oldSubs, seen, result); err != nil {
				return err
			}
		} else {
			seen[e.Hash] = struct{}{}
			*result = append(*result, e.Hash)
		}
	}

	return nil
}

// collectAllTreeObjects recursively walks a tree, adding all unseen
// tree and blob hashes to result. This is faster than collectChangedTreeObjects
// when we need all objects (no haves to diff against).
func collectAllTreeObjects(
	s storer.EncodedObjectStorer,
	t *object.Tree,
	seen map[plumbing.Hash]struct{},
	result *[]plumbing.Hash,
) error {
	if _, ok := seen[t.Hash]; ok {
		return nil
	}
	seen[t.Hash] = struct{}{}
	*result = append(*result, t.Hash)

	for _, e := range t.Entries {
		if e.Mode == filemode.Submodule {
			continue
		}
		if _, ok := seen[e.Hash]; ok {
			continue
		}
		if e.Mode == filemode.Dir {
			sub, err := object.GetTree(s, e.Hash)
			if err != nil {
				return fmt.Errorf("getting subtree %s: %w", e.Hash, err)
			}
			if err := collectAllTreeObjects(s, sub, seen, result); err != nil {
				return err
			}
		} else {
			seen[e.Hash] = struct{}{}
			*result = append(*result, e.Hash)
		}
	}
	return nil
}

// markTreeSeen recursively adds all tree and blob hashes in t to seen.
// Objects added to seen are not added to result — this is used to mark
// haves-reachable objects so they are skipped during diff walks.
func markTreeSeen(s storer.EncodedObjectStorer, t *object.Tree, seen map[plumbing.Hash]struct{}) {
	if _, ok := seen[t.Hash]; ok {
		return
	}
	seen[t.Hash] = struct{}{}
	for _, e := range t.Entries {
		if e.Mode == filemode.Submodule {
			continue
		}
		if _, ok := seen[e.Hash]; ok {
			continue
		}
		if e.Mode == filemode.Dir {
			if sub, err := object.GetTree(s, e.Hash); err == nil {
				markTreeSeen(s, sub, seen)
			}
		} else {
			seen[e.Hash] = struct{}{}
		}
	}
}
