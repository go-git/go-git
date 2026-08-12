package git

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

type objectWalker struct {
	Storer storage.Storer
	// seen is the set of objects seen in the repo.
	// seen map can become huge if walking over large
	// repos. Thus using struct{} as the value type.
	seen map[plumbing.Hash]struct{}
	// shallows is the set of shallow roots, loaded lazily
	// on the first commit walked.
	shallows map[plumbing.Hash]struct{}
	// promisor records that the repository is a partial clone, so an object
	// a promisor remote withheld is expected to be absent rather than a sign
	// of corruption. It makes the walk tolerate those absences, at the cost
	// of having to confirm that referenced blobs are present.
	promisor bool
	// missing is the set of objects that are referenced but absent. It is only
	// populated for a partial clone, where absences are expected; elsewhere a
	// missing object fails the walk. Callers that write objects out have to
	// exclude these, since there is nothing local to write.
	missing map[plumbing.Hash]struct{}
}

func newObjectWalker(s storage.Storer) *objectWalker {
	return &objectWalker{
		Storer:   s,
		seen:     map[plumbing.Hash]struct{}{},
		promisor: isPartialClone(s),
		missing:  map[plumbing.Hash]struct{}{},
	}
}

// isPartialClone reports whether the repository holds packs fetched from a
// promisor remote, which is what makes referenced-but-absent objects expected.
//
// Errors are treated as "not a partial clone": that keeps the stricter walk,
// which fails loudly on a missing object rather than quietly tolerating it.
func isPartialClone(s storage.Storer) bool {
	pos, ok := s.(storer.PromisorObjectStorer)
	if !ok {
		return false
	}

	packs, err := pos.PromisorObjectPacks()
	return err == nil && len(packs) > 0
}

// present returns the seen objects that are actually stored locally, which is
// what can be written back out to a new pack.
func (p *objectWalker) present() []plumbing.Hash {
	objs := make([]plumbing.Hash, 0, len(p.seen)-len(p.missing))
	for h := range p.seen {
		if _, absent := p.missing[h]; absent {
			continue
		}
		objs = append(objs, h)
	}
	return objs
}

// isShallow reports whether hash is a shallow root, meaning its
// parents are not present in the repository.
func (p *objectWalker) isShallow(hash plumbing.Hash) (bool, error) {
	if p.shallows == nil {
		shallows, err := p.Storer.Shallow()
		if err != nil {
			return false, err
		}
		p.shallows = make(map[plumbing.Hash]struct{}, len(shallows))
		for _, h := range shallows {
			p.shallows[h] = struct{}{}
		}
	}
	_, ok := p.shallows[hash]
	return ok, nil
}

// walkAllRefs walks all (hash) references from the repo.
func (p *objectWalker) walkAllRefs() error {
	// Walk over all the references in the repo.
	it, err := p.Storer.IterReferences()
	if err != nil {
		return err
	}
	defer it.Close()
	err = it.ForEach(func(ref *plumbing.Reference) error {
		// Exit this iteration early for non-hash references.
		if ref.Type() != plumbing.HashReference {
			return nil
		}
		return p.walkObjectTree(ref.Hash())
	})
	return err
}

func (p *objectWalker) isSeen(hash plumbing.Hash) bool {
	_, seen := p.seen[hash]
	return seen
}

func (p *objectWalker) add(hash plumbing.Hash) {
	p.seen[hash] = struct{}{}
}

// walkObjectTree walks over all objects and remembers references
// to them in the objectWalker. This is used instead of the revlist
// walks because memory usage is tight with huge repos.
func (p *objectWalker) walkObjectTree(hash plumbing.Hash) error {
	// Check if we have already seen, and mark this object
	if p.isSeen(hash) {
		return nil
	}
	p.add(hash)
	// Fetch the object.
	obj, err := object.GetObject(p.Storer, hash)
	if err != nil {
		// In a partial clone the promisor remote withheld objects on
		// purpose, so an absent one is expected. Record it and stop
		// descending: there is nothing local to walk into.
		if p.promisor && errors.Is(err, plumbing.ErrObjectNotFound) {
			p.missing[hash] = struct{}{}
			return nil
		}
		return fmt.Errorf("getting object %s failed: %w", hash, err)
	}
	// Walk all children depending on object type.
	switch obj := obj.(type) {
	case *object.Commit:
		err = p.walkObjectTree(obj.TreeHash)
		if err != nil {
			return err
		}
		// Parents of a shallow root are not present in the
		// repository, so don't attempt to walk them.
		shallow, err := p.isShallow(obj.ID())
		if err != nil {
			return err
		}
		if shallow {
			break
		}
		for _, h := range obj.ParentHashes {
			err = p.walkObjectTree(h)
			if err != nil {
				return err
			}
		}
	case *object.Tree:
		for i := range obj.Entries {
			// Shortcut for blob objects:
			// 'or' the lower bits of a mode and check that it
			// it matches a filemode.Executable. The type information
			// is in the higher bits, but this is the cleanest way
			// to handle plain files with different modes.
			// Other non-tree objects are somewhat rare, so they
			// are not special-cased.
			if obj.Entries[i].Mode|0o755 == filemode.Executable {
				h := obj.Entries[i].Hash
				p.add(h)
				// The shortcut takes a tree entry's word for it that
				// the blob exists, which a partial clone cannot afford:
				// blob:none withholds exactly these, and treating them
				// as present makes a later encode fail on an object
				// that was never fetched. Only the filtered case pays
				// for the lookup.
				if p.promisor {
					if _, err := p.Storer.EncodedObjectSize(h); err != nil {
						if !errors.Is(err, plumbing.ErrObjectNotFound) {
							return err
						}
						p.missing[h] = struct{}{}
					}
				}
				continue
			}
			// Normal walk for sub-trees (and symlinks etc).
			err = p.walkObjectTree(obj.Entries[i].Hash)
			if err != nil {
				return err
			}
		}
	case *object.Tag:
		return p.walkObjectTree(obj.Target)
	default:
		// Error out on unhandled object types.
		return fmt.Errorf("unknown object %X %s %T", obj.ID(), obj.Type(), obj)
	}
	return nil
}
