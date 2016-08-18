package core

import (
	"errors"
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

// AsRemote returns a new remote reference name using current one as base
func (r ReferenceName) AsRemote(remote string) ReferenceName {
	return ReferenceName(refRemotePrefix + remote + "/" + r.alias())
}

func (r ReferenceName) String() string {
	return string(r)
}

func (r ReferenceName) alias() string {
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
	return strings.HasPrefix(string(r.n), refHeadPrefix) || r.n == HEAD
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
// the iteration is stop but no error is returned
func (iter *ReferenceSliceIter) ForEach(cb func(*Reference) error) error {
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

const (
	refSpecWildcard  = "*"
	refSpecForce     = "+"
	refSpecSeparator = ":"
)

// RefSpec is a mapping from local branches to remote references
// The format of the refspec is an optional +, followed by <src>:<dst>, where
// <src> is the pattern for references on the remote side and <dst> is where
// those references will be written locally. The + tells Git to update the
// reference even if it isn’t a fast-forward.
// eg.: "+refs/heads/*:refs/remotes/origin/*"
//
// https://git-scm.com/book/es/v2/Git-Internals-The-Refspec
type RefSpec string

// IsValid validates the RefSpec
func (s RefSpec) IsValid() bool {
	spec := string(s)
	if strings.Count(spec, refSpecSeparator) != 1 {
		return false
	}

	sep := strings.Index(spec, refSpecSeparator)
	if sep == len(spec) {
		return false
	}

	ws := strings.Count(spec[0:sep], refSpecWildcard)
	wd := strings.Count(spec[sep+1:len(spec)], refSpecWildcard)
	return ws == wd && ws < 2 && wd < 2
}

// IsForceUpdate returns if update is allowed in non fast-forward merges
func (s RefSpec) IsForceUpdate() bool {
	if s[0] == refSpecForce[0] {
		return true
	}

	return false
}

// Src return the src side
func (s RefSpec) Src() string {
	spec := string(s)
	start := strings.Index(spec, refSpecForce) + 1
	end := strings.Index(spec, refSpecSeparator)

	return spec[start:end]
}

// Match match the given ReferenceName against the source
func (s RefSpec) Match(n ReferenceName) bool {
	if !s.isGlob() {
		return s.matchExact(n)
	}

	return s.matchGlob(n)
}

func (s RefSpec) isGlob() bool {
	return strings.Index(string(s), refSpecWildcard) != -1
}

func (s RefSpec) matchExact(n ReferenceName) bool {
	return s.Src() == n.String()
}

func (s RefSpec) matchGlob(n ReferenceName) bool {
	src := s.Src()
	name := n.String()
	wildcard := strings.Index(src, refSpecWildcard)

	var prefix, suffix string
	prefix = src[0:wildcard]
	if len(src) < wildcard {
		suffix = src[wildcard+1 : len(suffix)]
	}

	return len(name) > len(prefix)+len(suffix) &&
		strings.HasPrefix(name, prefix) &&
		strings.HasSuffix(name, suffix)
}

// Dst returns the destination for the given remote reference
func (s RefSpec) Dst(n ReferenceName) ReferenceName {
	spec := string(s)
	start := strings.Index(spec, refSpecSeparator) + 1
	dst := spec[start:len(spec)]
	src := s.Src()

	if !s.isGlob() {
		return ReferenceName(dst)
	}

	name := n.String()
	ws := strings.Index(src, refSpecWildcard)
	wd := strings.Index(dst, refSpecWildcard)
	match := name[ws : len(name)-(len(src)-(ws+1))]

	return ReferenceName(dst[0:wd] + match + dst[wd+1:len(dst)])

}
