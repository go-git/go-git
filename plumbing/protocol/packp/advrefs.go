// Package packp implements encoding and decoding of the Git packfile protocol messages.
package packp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// AdvRefs values represent the information transmitted on an
// advertised-refs message. The zero value is safe to use; References
// and Shallows can be populated via append.
type AdvRefs struct {
	// Capabilities are the capabilities.
	Capabilities capability.List
	// References are the hash references, including HEAD and peeled refs
	// (whose names end in ^{}). They are stored in wire order.
	References []*plumbing.Reference
	// Shallows are the shallow object ids.
	Shallows []plumbing.Hash
}

// Head returns the HEAD reference. It checks the first reference in
// References (HEAD is always first on the wire) before scanning the rest.
func (a *AdvRefs) Head() (*plumbing.Reference, error) {
	if len(a.References) > 0 && a.References[0].Name() == plumbing.HEAD {
		return a.References[0], nil
	}
	for _, ref := range a.References {
		if ref.Name() == plumbing.HEAD {
			return ref, nil
		}
	}
	return nil, plumbing.ErrReferenceNotFound
}

// ResolvedHead returns HEAD as a SymbolicReference when possible. If the
// symref capability is present it is used; otherwise the heuristic
// described in resolvedHeadFromHeuristic is applied. If HEAD cannot be
// resolved it is returned as-is (a HashReference). Returns
// ErrReferenceNotFound if HEAD is not present in References.
func (a *AdvRefs) ResolvedHead() (*plumbing.Reference, error) {
	head, err := a.Head()
	if err != nil {
		return nil, err
	}

	if head.Type() == plumbing.SymbolicReference {
		return head, nil
	}

	if a.supportSymrefs() {
		return a.resolvedHeadFromSymref(head)
	}

	return a.resolvedHeadFromHeuristic(head), nil
}

// resolvedHeadFromSymref resolves HEAD using the symref capability.
// Returns head unchanged if no HEAD entry exists in the symref map.
func (a *AdvRefs) resolvedHeadFromSymref(head *plumbing.Reference) (*plumbing.Reference, error) {
	symrefs, err := a.symRefMap()
	if err != nil {
		return nil, err
	}
	if target, ok := symrefs[plumbing.HEAD]; ok {
		return plumbing.NewSymbolicReference(plumbing.HEAD, target), nil
	}
	return head, nil
}

// ResolvedReferences returns all references with HEAD resolved to a
// SymbolicReference when possible, and symref capabilities applied to
// other references. The result is sorted by reference name.
func (a *AdvRefs) ResolvedReferences() ([]*plumbing.Reference, error) {
	refs := make([]*plumbing.Reference, len(a.References))
	copy(refs, a.References)

	symrefs, err := a.symRefMap()
	if err != nil {
		return nil, err
	}
	if a.supportSymrefs() {
		for name, target := range symrefs {
			symRef := plumbing.NewSymbolicReference(name, target)
			found := false
			for i, ref := range refs {
				if ref.Name() == name {
					refs[i] = symRef
					found = true
					break
				}
			}
			if !found {
				refs = append(refs, symRef)
			}
		}
	} else {
		for i, ref := range refs {
			if ref.Name() == plumbing.HEAD && ref.Type() == plumbing.HashReference {
				refs[i] = a.resolvedHeadFromHeuristic(ref)
				break
			}
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Name() < refs[j].Name()
	})
	return refs, nil
}

// symRefMap parses the symref capability values into a name→target map.
func (a *AdvRefs) symRefMap() (map[plumbing.ReferenceName]plumbing.ReferenceName, error) {
	symrefs := a.Capabilities.Get(capability.SymRef)
	m := make(map[plumbing.ReferenceName]plumbing.ReferenceName, len(symrefs))
	for _, symref := range symrefs {
		chunks := strings.Split(symref, ":")
		if len(chunks) != 2 {
			return nil, fmt.Errorf("bad number of `:` in symref value (%q)", symref)
		}
		m[plumbing.ReferenceName(chunks[0])] = plumbing.ReferenceName(chunks[1])
	}
	return m, nil
}

// resolvedHeadFromHeuristic tries to convert HEAD from a HashReference to
// a SymbolicReference pointing to the branch that shares its hash.
//
// If the server does not support the symref capability, git versions
// prior to 1.8.4.3 used this heuristic:
//   - Check if master exists and has the same hash as HEAD.
//   - If not, scan references in alphabetical order for a matching hash.
//   - If no match is found, HEAD is returned unchanged.
func (a *AdvRefs) resolvedHeadFromHeuristic(head *plumbing.Reference) *plumbing.Reference {
	headHash := head.Hash()

	for _, ref := range a.References {
		if ref.Name() == plumbing.Master && ref.Type() == plumbing.HashReference && ref.Hash() == headHash {
			return plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)
		}
	}

	candidates := make([]*plumbing.Reference, 0, len(a.References))
	for _, ref := range a.References {
		if ref.Name() == plumbing.HEAD || ref.Name().IsPeeled() {
			continue
		}
		if ref.Type() == plumbing.HashReference && ref.Hash() == headHash {
			candidates = append(candidates, ref)
		}
	}
	if len(candidates) > 0 {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Name() < candidates[j].Name()
		})
		return plumbing.NewSymbolicReference(plumbing.HEAD, candidates[0].Name())
	}

	return head
}

// IsEmpty returns true if doesn't contain any reference.
func (a *AdvRefs) IsEmpty() bool {
	return len(a.References) == 0 &&
		len(a.Shallows) == 0
}

func (a *AdvRefs) supportSymrefs() bool {
	return a.Capabilities.Supports(capability.SymRef)
}
