package core

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	refPrefix       = "refs/"
	refHeadPrefix   = refPrefix + "heads/"
	refTagPrefix    = refPrefix + "tags/"
	refRemotePrefix = refPrefix + "remotes/"
	refNotePrefix   = refPrefix + "notes/"
	symrefPrefix    = "ref: "

	maxResolveRecursion = 1024
)

var (
	ErrMaxResolveRecursion = errors.New("max. recursion level reached")
	ErrReferenceNotFound   = errors.New("reference not found")
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

func (r ReferenceName) String() string {
	return string(r)
}

// Short returns the short name of a ReferenceName
func (r ReferenceName) Short() string {
	parts := strings.Split(string(r), "/")
	return parts[len(parts)-1]
}

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
	n := ReferenceName(name)

	if strings.HasPrefix(target, symrefPrefix) {
		target := ReferenceName(target[len(symrefPrefix):])
		return NewSymbolicReference(n, target)
	}

	return NewHashReference(n, NewHash(target))
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

// Strings dump a reference as a [2]string
func (r *Reference) Strings() [2]string {
	var o [2]string
	o[0] = r.Name().String()

	switch r.Type() {
	case HashReference:
		o[1] = r.Hash().String()
	case SymbolicReference:
		o[1] = symrefPrefix + r.Target().String()
	}

	return o
}

func (r *Reference) String() string {
	s := r.Strings()
	return fmt.Sprintf("%s %s", s[1], s[0])
}

// ReferenceSliceIter implements ReferenceIter. It iterates over a series of
// references stored in a slice and yields each one in turn when Next() is
// called.
//
// The ReferenceSliceIter must be closed with a call to Close() when it is no
// longer needed.
type ReferenceSliceIter struct {
	series []*Reference
	pos    int
}

// NewReferenceSliceIter returns a reference iterator for the given slice of
// objects.
func NewReferenceSliceIter(series []*Reference) *ReferenceSliceIter {
	return &ReferenceSliceIter{
		series: series,
	}
}

// Next returns the next reference from the iterator. If the iterator has
// reached the end it will return io.EOF as an error.
func (iter *ReferenceSliceIter) Next() (*Reference, error) {
	if iter.pos >= len(iter.series) {
		return nil, io.EOF
	}

	obj := iter.series[iter.pos]
	iter.pos++
	return obj, nil
}

// ForEach call the cb function for each reference contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *ReferenceSliceIter) ForEach(cb func(*Reference) error) error {
	defer iter.Close()
	for _, r := range iter.series {
		if err := cb(r); err != nil {
			if err == ErrStop {
				return nil
			}

			return nil
		}
	}

	return nil
}

// Close releases any resources used by the iterator.
func (iter *ReferenceSliceIter) Close() {
	iter.pos = len(iter.series)
}

func ResolveReference(s ReferenceStorage, n ReferenceName) (*Reference, error) {
	r, err := s.Get(n)
	if err != nil || r == nil {
		return r, err
	}
	return resolveReference(s, r, 0)
}

func resolveReference(s ReferenceStorage, r *Reference, recursion int) (*Reference, error) {
	if r.Type() != SymbolicReference {
		return r, nil
	}

	if recursion > maxResolveRecursion {
		return nil, ErrMaxResolveRecursion
	}

	t, err := s.Get(r.Target())
	if err != nil {
		return nil, err
	}

	recursion++
	return resolveReference(s, t, recursion)
}
