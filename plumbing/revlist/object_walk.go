package revlist

import (
	"errors"
	"fmt"
	"slices"
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
	// seen holds objects that are already in result or excluded by the
	// haves side (haves tips and edge parents), with the type they were
	// seen as: their own for commits and tags, the one their tree entry
	// mode implies for trees and blobs. A seen tree is never walked
	// again: everything below it is seen too, or could not be read.
	// Anything that adds a tree here must uphold this.
	seen   map[plumbing.Hash]plumbing.ObjectType
	result []plumbing.Hash
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
		seen:      make(map[plumbing.Hash]plumbing.ObjectType),
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
			w.seen[tag.Hash] = plumbing.TagObject
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
			w.seen[h] = plumbing.BlobObject
			w.result = append(w.result, h)
		default:
			return fmt.Errorf("unsupported object type %s for %s", o.Type(), h)
		}
	}
	return nil
}

// seedHaves enqueues each have commit and pre-populates seen with all
// tree/blob objects reachable from the haves tips. Non-commit objects
// (tags, trees, blobs) are marked as seen so the tree walk skips them.
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
			w.seen[tag.Hash] = plumbing.TagObject
			haves = append(haves, tag.Target)
		case plumbing.TreeObject:
			if t, err := object.GetTree(w.s, h); err == nil {
				markTreeSeen(w.s, t, w.seen)
			}
		case plumbing.BlobObject:
			w.seen[h] = plumbing.BlobObject
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

	// Phase 2: drop new commits that were painted by haves after being
	// added. Then, like Git's mark_edges_uninteresting, mark the trees
	// of all edge parents (parents of new commits painted by haves) as
	// seen before collecting any new tree, so that objects moved or
	// copied from them are not sent. Shallow commits have no parents
	// here, as with Git's grafts.
	newCommits = slices.DeleteFunc(newCommits, func(c *object.Commit) bool {
		return flags[c.Hash]&havePaint != 0
	})
	for _, lc := range newCommits {
		if _, shallow := w.shallows[lc.Hash]; shallow {
			continue
		}
		for _, ph := range lc.ParentHashes {
			if flags[ph]&havePaint == 0 {
				continue
			}
			if err := w.markEdgeTreeSeen(ph); err != nil {
				return err
			}
		}
	}
	for _, lc := range newCommits {
		if err := w.collectCommit(lc); err != nil {
			return err
		}
	}
	return nil
}

// markEdgeTreeSeen marks the tree of edge parent h as seen. A missing
// edge parent is tolerated, as it may be beyond the haves boundary, but
// the root tree of one that is present must be readable.
func (w *objectWalk) markEdgeTreeSeen(h plumbing.Hash) error {
	c, err := object.GetCommit(w.s, h)
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil
		}
		return fmt.Errorf("getting parent commit %s: %w", h, err)
	}
	if _, ok := w.seen[c.TreeHash]; ok {
		return nil // shared with an edge already marked
	}
	t, err := c.Tree()
	if err != nil {
		return fmt.Errorf("getting parent tree for %s: %w", h, err)
	}
	markTreeSeen(w.s, t, w.seen)
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

// walkFull is the fast path when there are no haves. It walks every
// commit reachable from the wants and collects all of their trees and
// blobs, without painting.
func (w *objectWalk) walkFull() error {
	for len(w.wantsQueue) > 0 {
		lc := w.wantsQueue[0]
		w.wantsQueue = w.wantsQueue[1:]

		if _, ok := w.seen[lc.Hash]; ok {
			continue
		}
		if err := w.collectCommit(lc); err != nil {
			return err
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

// collectCommit adds lc and every tree and blob reachable from it that
// is not already seen to the result.
func (w *objectWalk) collectCommit(lc *object.Commit) error {
	if _, ok := w.seen[lc.Hash]; !ok {
		w.seen[lc.Hash] = plumbing.CommitObject
		w.result = append(w.result, lc.Hash)
	}

	tree, err := lc.Tree()
	if err != nil {
		return fmt.Errorf("getting tree for %s: %w", lc.Hash, err)
	}

	if err := collectAllTreeObjects(w.s, tree, w.seen, &w.result); err != nil {
		return fmt.Errorf("collecting tree objects for %s: %w", lc.Hash, err)
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

// collectAllTreeObjects recursively walks a tree, adding all unseen
// tree and blob hashes to result. A seen tree is skipped without
// descending into it, as all of its contents are seen as well. An
// object seen as a tree and used as a blob, or the other way around,
// fails with plumbing.ErrInvalidType, as it does in Git.
func collectAllTreeObjects(
	s storer.EncodedObjectStorer,
	t *object.Tree,
	seen map[plumbing.Hash]plumbing.ObjectType,
	result *[]plumbing.Hash,
) error {
	if typ, ok := seen[t.Hash]; ok {
		return checkSeenType(t.Hash, typ, plumbing.TreeObject)
	}
	seen[t.Hash] = plumbing.TreeObject
	*result = append(*result, t.Hash)

	for _, e := range t.Entries {
		if e.Mode == filemode.Submodule {
			continue
		}
		typ := entryType(e.Mode)
		if seenTyp, ok := seen[e.Hash]; ok {
			if err := checkSeenType(e.Hash, seenTyp, typ); err != nil {
				return err
			}
			continue
		}
		if typ == plumbing.TreeObject {
			sub, err := object.GetTree(s, e.Hash)
			if err != nil {
				return fmt.Errorf("getting subtree %s: %w", e.Hash, err)
			}
			if err := collectAllTreeObjects(s, sub, seen, result); err != nil {
				return err
			}
		} else {
			seen[e.Hash] = plumbing.BlobObject
			*result = append(*result, e.Hash)
		}
	}
	return nil
}

// markTreeSeen adds t and every tree and blob below it to seen, without
// adding them to result. It excludes the objects reachable from haves
// tips and edge parents. As Git's mark_tree_uninteresting does for edge
// parents, a subtree that cannot be read is still marked by hash, but
// not descended into, so it is neither read nor sent when a want
// references it. An object already seen as another type keeps that
// type, so collectAllTreeObjects reports the conflict if a want uses it.
func markTreeSeen(s storer.EncodedObjectStorer, t *object.Tree, seen map[plumbing.Hash]plumbing.ObjectType) {
	if _, ok := seen[t.Hash]; ok {
		return
	}
	seen[t.Hash] = plumbing.TreeObject
	for _, e := range t.Entries {
		if e.Mode == filemode.Submodule {
			continue
		}
		if _, ok := seen[e.Hash]; ok {
			continue
		}
		typ := entryType(e.Mode)
		if typ == plumbing.TreeObject {
			sub, err := object.GetTree(s, e.Hash)
			if err != nil {
				seen[e.Hash] = plumbing.TreeObject
				continue
			}
			markTreeSeen(s, sub, seen)
		} else {
			seen[e.Hash] = plumbing.BlobObject
		}
	}
}

// entryType returns the object type a tree entry with mode m refers to.
// Submodule entries refer to commits in another repository and are
// skipped by the callers.
func entryType(m filemode.FileMode) plumbing.ObjectType {
	if m == filemode.Dir {
		return plumbing.TreeObject
	}
	return plumbing.BlobObject
}

// checkSeenType returns an error when h, seen as type seen, is used as
// type used.
func checkSeenType(h plumbing.Hash, seen, used plumbing.ObjectType) error {
	if seen == used {
		return nil
	}
	return fmt.Errorf("%w: %s is used as both a %s and a %s", plumbing.ErrInvalidType, h, seen, used)
}
