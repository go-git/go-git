package core

import "strings"

const (
	refPrefix       = "refs/"
	refHeadPrefix   = refPrefix + "heads/"
	refTagPrefix    = refPrefix + "tags/"
	refRemotePrefix = refPrefix + "remotes/"
	refNotePrefix   = refPrefix + "notes/"
	symrefPrefix    = "ref: "
)

// ReferenceType reference type's
type ReferenceType int8

const (
	InvalidReference  ReferenceType = 0
	HashReference     ReferenceType = 1
	SymbolicReference ReferenceType = 2
)

// ReferenceName reference name's
type ReferenceName string

const (
	HEAD ReferenceName = "HEAD"
)

// Reference is a representation of git reference
type Reference struct {
	t      ReferenceType
	n      ReferenceName
	h      Hash
	target ReferenceName
}

// NewReferenceFromStrings creates a reference from name and target as string,
// the resulting reference can be a SymbolicReference or a HashReference base
// on the target provided
func NewReferenceFromStrings(name, target string) *Reference {
	r := &Reference{n: ReferenceName(name)}

	if strings.HasPrefix(target, symrefPrefix) {
		r.t = SymbolicReference
		r.target = ReferenceName(target[len(symrefPrefix):])
		return r
	}

	r.t = HashReference
	r.h = NewHash(target)
	return r
}

// NewSymbolicReference creates a new SymbolicReference reference
func NewSymbolicReference(n, target ReferenceName) *Reference {
	return &Reference{
		t:      SymbolicReference,
		n:      n,
		target: target,
	}
}

// NewHashReference creates a new HashReference reference
func NewHashReference(n ReferenceName, h Hash) *Reference {
	return &Reference{
		t: HashReference,
		n: n,
		h: h,
	}
}

// Type return the type of a reference
func (r *Reference) Type() ReferenceType {
	return r.t
}

// Name return the name of a reference
func (r *Reference) Name() ReferenceName {
	return r.n
}

// Hash return the hash of a hash reference
func (r *Reference) Hash() Hash {
	return r.h
}

// Target return the target of a symbolic reference
func (r *Reference) Target() ReferenceName {
	return r.target
}

// IsBranch check if a reference is a branch
func (r *Reference) IsBranch() bool {
	return strings.HasPrefix(string(r.n), refHeadPrefix)
}

// IsNote check if a reference is a note
func (r *Reference) IsNote() bool {
	return strings.HasPrefix(string(r.n), refNotePrefix)
}

// IsRemote check if a reference is a remote
func (r *Reference) IsRemote() bool {
	return strings.HasPrefix(string(r.n), refRemotePrefix)
}

// IsTag check if a reference is a tag
func (r *Reference) IsTag() bool {
	return strings.HasPrefix(string(r.n), refTagPrefix)
}

// ReferenceStorage generic storage of references
type ReferenceStorage interface {
	Set(Reference) error
	Get(ReferenceName) (Reference, error)
	Iter(ObjectType) (ReferenceIter, error)
}

// ReferenceIter is a generic closable interface for iterating over references
type ReferenceIter interface {
	Next() (Reference, error)
	Close()
}
