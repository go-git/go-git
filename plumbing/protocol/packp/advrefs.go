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
	// DefaultBranch is the branch the detached-HEAD heuristic should prefer
	// (the client's init.defaultBranch, as a full ref name), tried before
	// refs/heads/master. Empty disables the preference.
	DefaultBranch plumbing.ReferenceName
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
	return ResolveHeadFromHashHeuristic(head, a.References, a.DefaultBranch)
}

// ResolveHeadFromHashHeuristic converts a detached (HashReference) HEAD into a
// SymbolicReference pointing at a branch that shares its hash, mirroring
// upstream's guess_remote_head (remote.c): prefer defaultBranch (the client's
// init.defaultBranch), then refs/heads/master, then the first advertised ref
// that points there (in wire order). HEAD is returned unchanged if no ref
// matches. Used by both the v0/v1 advertisement and the protocol v2 ls-refs
// path, which only emits a symref-target for a symbolic HEAD.
func ResolveHeadFromHashHeuristic(head *plumbing.Reference, refs []*plumbing.Reference, defaultBranch plumbing.ReferenceName) *plumbing.Reference {
	headHash := head.Hash()

	for _, name := range []plumbing.ReferenceName{defaultBranch, plumbing.Master} {
		if name == "" {
			continue
		}
		for _, ref := range refs {
			if ref.Name() == name && ref.Type() == plumbing.HashReference && ref.Hash() == headHash {
				return plumbing.NewSymbolicReference(plumbing.HEAD, name)
			}
		}
	}

	// No preferred branch matched; take the first advertised ref that points
	// at HEAD's hash, preserving wire order as upstream does.
	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD || ref.Name().IsPeeled() {
			continue
		}
		if ref.Type() == plumbing.HashReference && ref.Hash() == headHash {
			return plumbing.NewSymbolicReference(plumbing.HEAD, ref.Name())
		}
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
