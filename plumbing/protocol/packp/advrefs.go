package packp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

// AdvRefs values represent the information transmitted on an
// advertised-refs message.  Values from this type are not zero-value
// safe, use the New function instead.
type AdvRefs struct {
	// Head stores the resolved HEAD reference if present.
	// This can be present with git-upload-pack, not with git-receive-pack.
	Head *plumbing.Hash
	// Capabilities are the capabilities.
	Capabilities *capability.List
	// References are the hash references.
	References map[string]plumbing.Hash
	// Peeled are the peeled hash references.
	Peeled map[string]plumbing.Hash
	// Shallows are the shallow object ids.
	Shallows []plumbing.Hash
}

// NewAdvRefs returns a pointer to a new AdvRefs value, ready to be used.
func NewAdvRefs() *AdvRefs {
	return &AdvRefs{
		Capabilities: capability.NewList(),
		References:   make(map[string]plumbing.Hash),
		Peeled:       make(map[string]plumbing.Hash),
		Shallows:     []plumbing.Hash{},
	}
}

func (a *AdvRefs) AddReference(r *plumbing.Reference) error {
	switch r.Type() {
	case plumbing.SymbolicReference:
		v := fmt.Sprintf("%s:%s", r.Name().String(), r.Target().String())
		return a.Capabilities.Add(capability.SymRef, v)
	case plumbing.HashReference:
		a.References[r.Name().String()] = r.Hash()
	default:
		return plumbing.ErrInvalidType
	}

	return nil
}

// XXX: AllReferences doesn't return all the references advertised by the
// server, instead, it only returns non-peeled references.
// Use MakeReferenceSlice to get all the references, their peeled values, and
// symrefs.
func (a *AdvRefs) AllReferences() (memory.ReferenceStorage, error) {
	s := memory.ReferenceStorage{}
	if err := a.addRefs(s); err != nil {
		return s, plumbing.NewUnexpectedError(err)
	}

	return s, nil
}

func (a *AdvRefs) addRefs(s storer.ReferenceStorer) error {
	for name, hash := range a.References {
		ref := plumbing.NewReferenceFromStrings(name, hash.String())
		if err := s.SetReference(ref); err != nil {
			return err
		}
	}

	if a.supportSymrefs() {
		return a.addSymbolicRefs(s)
	}

	return a.resolveHead(s)
}

// If the server does not support symrefs capability,
// we need to guess the reference where HEAD is pointing to.
//
// Git versions prior to 1.8.4.3 has an special procedure to get
// the reference where is pointing to HEAD:
//   - Check if a reference called master exists. If exists and it
//     has the same hash as HEAD hash, we can say that HEAD is pointing to master
//   - If master does not exists or does not have the same hash as HEAD,
//     order references and check in that order if that reference has the same
//     hash than HEAD. If yes, set HEAD pointing to that branch hash
//   - If no reference is found, throw an error
func (a *AdvRefs) resolveHead(s storer.ReferenceStorer) error {
	if a.Head == nil {
		return nil
	}

	ref, err := s.Reference(plumbing.Master)

	// check first if HEAD is pointing to master
	if err == nil {
		ok, err := a.createHeadIfCorrectReference(ref, s)
		if err != nil {
			return err
		}

		if ok {
			return nil
		}
	}

	if err != nil && err != plumbing.ErrReferenceNotFound {
		return err
	}

	// From here we are trying to guess the branch that HEAD is pointing
	refIter, err := s.IterReferences()
	if err != nil {
		return err
	}

	var refNames []string
	err = refIter.ForEach(func(r *plumbing.Reference) error {
		refNames = append(refNames, string(r.Name()))
		return nil
	})
	if err != nil {
		return err
	}

	sort.Strings(refNames)

	var headSet bool
	for _, refName := range refNames {
		ref, err := s.Reference(plumbing.ReferenceName(refName))
		if err != nil {
			return err
		}
		ok, err := a.createHeadIfCorrectReference(ref, s)
		if err != nil {
			return err
		}
		if ok {
			headSet = true
			break
		}
	}

	if !headSet {
		return plumbing.ErrReferenceNotFound
	}

	return nil
}

func (a *AdvRefs) createHeadIfCorrectReference(
	reference *plumbing.Reference,
	s storer.ReferenceStorer,
) (bool, error) {
	if reference.Hash() == *a.Head {
		headRef := plumbing.NewSymbolicReference(plumbing.HEAD, reference.Name())
		if err := s.SetReference(headRef); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

func (a *AdvRefs) addSymbolicRefs(s storer.ReferenceStorer) error {
	for _, symref := range a.Capabilities.Get(capability.SymRef) {
		chunks := strings.Split(symref, ":")
		if len(chunks) != 2 {
			err := fmt.Errorf("bad number of `:` in symref value (%q)", symref)
			return plumbing.NewUnexpectedError(err)
		}
		name := plumbing.ReferenceName(chunks[0])
		target := plumbing.ReferenceName(chunks[1])
		ref := plumbing.NewSymbolicReference(name, target)
		if err := s.SetReference(ref); err != nil {
			return nil
		}
	}

	return nil
}

func (a *AdvRefs) supportSymrefs() bool {
	return a.Capabilities.Supports(capability.SymRef)
}

// IsEmpty returns true if doesn't contain any reference.
func (a *AdvRefs) IsEmpty() bool {
	return a.Head == nil &&
		len(a.References) == 0 &&
		len(a.Peeled) == 0 &&
		len(a.Shallows) == 0
}

// MakeReferenceSlice returns a sorted slice with all the references, their
// peeled values, and symrefs.
func (a *AdvRefs) MakeReferenceSlice() ([]*plumbing.Reference, error) {
	refs, err := a.AllReferences()
	if err != nil {
		return nil, err
	}
	allRefs := make([]*plumbing.Reference, 0, len(refs))

	for _, ref := range refs {
		allRefs = append(allRefs, ref)
		if peeled, ok := a.Peeled[ref.Name().String()]; ok {
			peeledRef := plumbing.NewReferenceFromStrings(ref.Name().String()+"^{}", peeled.String())
			allRefs = append(allRefs, peeledRef)
		}
	}

	sort.Slice(allRefs, func(i, j int) bool {
		return allRefs[i].Name() < allRefs[j].Name()
	})

	return allRefs, nil
}
