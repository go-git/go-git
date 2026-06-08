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
	shallows   map[plumbing.Hash]struct{}
	wantsQueue []*object.Commit
	havesQueue []*object.Commit
	wantsSeen  map[plumbing.Hash]struct{}
	havesSeen  map[plumbing.Hash]struct{}
	seen       map[plumbing.Hash]struct{}
	result     []plumbing.Hash
}

func newObjectWalk(s storer.EncodedObjectStorer) (*objectWalk, error) {
	shallows, err := shallowSet(s)
	if err != nil {
		return nil, err
	}

	return &objectWalk{
		s:         s,
		shallows:  shallows,
		wantsSeen: make(map[plumbing.Hash]struct{}),
		havesSeen: make(map[plumbing.Hash]struct{}),
		seen:      make(map[plumbing.Hash]struct{}),
	}, nil
}

func shallowSet(s storer.EncodedObjectStorer) (map[plumbing.Hash]struct{}, error) {
	ss, ok := s.(storer.ShallowStorer)
	if !ok {
		return map[plumbing.Hash]struct{}{}, nil
	}

	hashes, err := ss.Shallow()
	if err != nil {
		return nil, err
	}

	set := make(map[plumbing.Hash]struct{}, len(hashes))
	for _, h := range hashes {
		set[h] = struct{}{}
	}

	return set, nil
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

// Paint flags for the commit walk. A commit reachable from both
// sides is a boundary; once all queue entries have both flags
// the walk can stop.
const (
	wantPaint uint8 = 1 << iota
	havePaint
)

// walk identifies new commits and collects their tree objects.
func (w *objectWalk) walk() error {
	// Fast path: no haves means we need all reachable objects.
	if len(w.havesQueue) == 0 {
		return w.walkFull()
	}

	// Phase 1: merge wants and haves into a single priority queue
	// sorted by committer time. Each commit carries paint flags that
	// propagate to parents. A commit painted from both sides is a
	// boundary. The walk stops when all queue entries are boundaries
	// (all stale), so only the overlap region is traversed.
	flags := make(map[plumbing.Hash]uint8)
	var queue []*object.Commit

	for _, c := range w.wantsQueue {
		flags[c.Hash] |= wantPaint
		insertSorted(&queue, c)
	}
	for _, c := range w.havesQueue {
		flags[c.Hash] |= havePaint
		insertSorted(&queue, c)
	}
	w.wantsQueue = nil
	w.havesQueue = nil

	var newCommits []*object.Commit
	var missing []missingParent

	for len(queue) > 0 {
		lc := queue[0]
		queue = queue[1:]

		f := flags[lc.Hash]

		// Want-only commit — tentatively new.
		if f == wantPaint {
			newCommits = append(newCommits, lc)
		}

		// Propagate this commit's flags to parents.
		if _, shallow := w.shallows[lc.Hash]; !shallow {
			if err := w.propagate(&queue, flags, &missing, lc, f); err != nil {
				return err
			}
		}

		// If all remaining queue entries have both flags, no new
		// commits can be discovered — stop early.
		if allStale(queue, flags) {
			break
		}
	}

	// Validate recorded missing parents now that all paint has settled.
	// A missing parent is tolerable only when its child has been painted
	// by haves (either directly, as a haves tip or ancestor, or via
	// later propagation that set havePaint on flags[child] before the
	// walk terminated). Otherwise the want side references history we
	// cannot traverse — match Git's behavior and error.
	for _, mp := range missing {
		if flags[mp.child]&havePaint != 0 {
			continue
		}
		return fmt.Errorf("commit %s has missing parent %s", mp.child, mp.hash)
	}

	// Phase 2: collect tree objects for new commits, skipping any
	// that were painted by haves after being added to newCommits.
	for _, lc := range newCommits {
		if flags[lc.Hash]&havePaint != 0 {
			continue
		}
		if err := w.processCommitTrees(lc); err != nil {
			return err
		}
	}
	return nil
}

// missingParent records a parent commit that could not be loaded during
// the painted walk, along with the child from which it was reached.
// Validation is deferred until the walk completes so that havePaint
// propagation from later iterations can mark the child (and its missing
// parent) as behind the haves boundary.
type missingParent struct {
	hash  plumbing.Hash
	child plumbing.Hash
}

// propagate adds the given flags to each parent commit. Parents that
// already have all the flags are skipped. Missing parents are not
// treated as fatal here; they are recorded and validated after the
// walk, where we can tell whether the child was eventually painted by
// haves (in which case the missing parent is behind the haves boundary
// and tolerable, matching Git's behavior).
func (w *objectWalk) propagate(queue *[]*object.Commit, flags map[plumbing.Hash]uint8, missing *[]missingParent, lc *object.Commit, f uint8) error {
	for _, ph := range lc.ParentHashes {
		pf := flags[ph]
		if pf|f == pf {
			continue // parent already has all our flags
		}
		flags[ph] = pf | f

		pc, err := object.GetCommit(w.s, ph)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				*missing = append(*missing, missingParent{hash: ph, child: lc.Hash})
				continue
			}
			return fmt.Errorf("getting parent commit %s: %w", ph, err)
		}
		insertSorted(queue, pc)
	}
	return nil
}

// allStale returns true when every commit in the queue has both paint
// flags, meaning all remaining commits are boundaries and no new
// commits can be discovered.
func allStale(queue []*object.Commit, flags map[plumbing.Hash]uint8) bool {
	for _, c := range queue {
		if flags[c.Hash]&wantPaint == 0 || flags[c.Hash]&havePaint == 0 {
			return false
		}
	}
	return true
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

		if _, ok := w.shallows[lc.Hash]; ok {
			continue
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

// processCommitTrees collects new tree/blob objects for a commit by
// diffing its tree against its parents' trees.
func (w *objectWalk) processCommitTrees(lc *object.Commit) error {
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
		parent, err := lc.Parent(i)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				continue // parent may be beyond haves boundary
			}
			return fmt.Errorf("getting parent commit %s: %w", lc.ParentHashes[i], err)
		}
		pt, err := parent.Tree()
		if err != nil {
			return fmt.Errorf("getting parent tree for %s: %w", parent.Hash, err)
		}
		oldTrees = append(oldTrees, pt)
	}

	if err := collectChangedTreeObjects(w.s, newTree, oldTrees, w.seen, &w.result); err != nil {
		return fmt.Errorf("diffing trees for %s: %w", lc.Hash, err)
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
