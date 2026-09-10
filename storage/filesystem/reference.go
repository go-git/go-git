package filesystem

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// ReferenceStorage implements storer.ReferenceStorer for filesystem storage.
//
// Writes require valid reference names. Reads and deletes use less restrictive
// path-safety checks so that existing names such as refs/heads/main.lock can be
// inspected and removed. IterReferences reports stored entries without applying
// either name check; an enumerated name is not necessarily readable or writable.
//
// Path-safety checks apply on every operating system. They reject backslashes,
// control characters, non-reference root names, and path components that HFS+
// or NTFS could interpret as "." or "..". For example, a component consisting
// of U+200C followed by a dot is rejected even on Linux. Existing references
// with these names cannot be read or removed through this storage API.
//
// To repair such a repository, use native Git on a filesystem that represents
// the name without aliasing (for example, git update-ref -d with the exact name
// on Linux). If native Git cannot remove it, back up the repository and remove
// the exact loose reference or packed-refs entry with filesystem tools while
// no process is modifying the repository. Do not normalize the name to another
// path: that could remove a different reference.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/Documentation/git-update-ref.adoc#L39-L40
type ReferenceStorage struct {
	dir *dotgit.DotGit
}

// SetReference stores a reference whose name passes the write checks described
// by ReferenceStorage. A rejected name returns an error wrapping
// plumbing.ErrInvalidReferenceName and dotgit.ErrReferenceNameEscape.
func (r *ReferenceStorage) SetReference(ref *plumbing.Reference) error {
	return r.dir.SetRef(ref, nil)
}

// CheckAndSetReference stores a reference after verifying the old value matches.
// It applies the same name checks as SetReference.
func (r *ReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	return r.dir.SetRef(ref, old)
}

// Reference returns the reference with the given name. Names rejected by the
// path-safety checks described by ReferenceStorage cannot be read, even if they
// are returned by IterReferences.
func (r *ReferenceStorage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	return r.dir.Ref(n)
}

// IterReferences returns an iterator over all references.
func (r *ReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	refs, err := r.dir.Refs()
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// RemoveReference deletes the reference with the given name. Invalid-format
// names such as refs/heads/main.lock can be removed, but names rejected by the
// path-safety checks described by ReferenceStorage cannot.
func (r *ReferenceStorage) RemoveReference(n plumbing.ReferenceName) error {
	return r.dir.RemoveRef(n)
}

// CountLooseRefs returns the number of loose references.
func (r *ReferenceStorage) CountLooseRefs() (int, error) {
	return r.dir.CountLooseRefs()
}

// PackRefs packs loose hash references with valid names and nonzero hashes into
// packed-refs. Symbolic references, malformed names and zero hashes remain loose.
// Existing packed references are retained unless replaced by a packed loose ref.
// It does not check whether referenced objects exist. The repository must not
// be modified concurrently while PackRefs runs.
func (r *ReferenceStorage) PackRefs() error {
	return r.dir.PackRefs()
}
