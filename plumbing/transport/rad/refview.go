package rad

import (
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// readOnlyRefs disables the reference-mutating methods of an embedded
// storage.Storer, so that view types built on top of it stay read-only
// regardless of what the underlying storer supports.
type readOnlyRefs struct {
	storage.Storer
}

// newReadOnly wraps base so that its references cannot be mutated, without
// otherwise restricting which of them are visible.
func newReadOnly(base storage.Storer) *readOnlyRefs {
	return &readOnlyRefs{Storer: base}
}

// Close releases the wrapped storer. storage.Storer does not embed
// io.Closer, so a view holding one as an interface field would not satisfy
// an io.Closer type assertion by itself — and the file transport this
// package composes releases its storer through exactly such an assertion
// (see file.Transport.connect). Forwarding Close here keeps that cleanup
// path working through the views; storers that are not closeable report
// success.
func (r *readOnlyRefs) Close() error {
	if c, ok := r.Storer.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// SetReference always fails: the rad transport is read-only.
func (r *readOnlyRefs) SetReference(*plumbing.Reference) error {
	return transport.ErrCommandUnsupported
}

// CheckAndSetReference always fails: the rad transport is read-only.
func (r *readOnlyRefs) CheckAndSetReference(new, old *plumbing.Reference) error {
	return transport.ErrCommandUnsupported
}

// RemoveReference always fails: the rad transport is read-only.
func (r *readOnlyRefs) RemoveReference(plumbing.ReferenceName) error {
	return transport.ErrCommandUnsupported
}

// canonical is a storage.Storer view exposing only the canonical references
// of a Radicle storage repository: HEAD, refs/heads/* and refs/tags/*. It
// hides refs/rad/* (Radicle identity and signed-refs metadata) and
// refs/namespaces/* (every peer's copy of their refs), which raw
// git-upload-pack would otherwise advertise wholesale. Objects, config,
// shallow and index storage pass straight through to the wrapped storer.
type canonical struct {
	readOnlyRefs
}

// newCanonical wraps base to expose only its canonical references.
func newCanonical(base storage.Storer) *canonical {
	return &canonical{readOnlyRefs{Storer: base}}
}

// isCanonical reports whether name is one this view advertises.
func isCanonical(name plumbing.ReferenceName) bool {
	return name == plumbing.HEAD ||
		strings.HasPrefix(name.String(), "refs/heads/") ||
		strings.HasPrefix(name.String(), "refs/tags/")
}

// Reference returns the canonical reference with the given name, or
// plumbing.ErrReferenceNotFound if name is not HEAD, refs/heads/* or
// refs/tags/*.
func (c *canonical) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	if !isCanonical(name) {
		return nil, plumbing.ErrReferenceNotFound
	}
	return c.Storer.Reference(name)
}

// IterReferences returns an iterator over HEAD, refs/heads/* and
// refs/tags/*, hiding refs/rad/* and refs/namespaces/*.
func (c *canonical) IterReferences() (storer.ReferenceIter, error) {
	it, err := c.Storer.IterReferences()
	if err != nil {
		return nil, err
	}

	var refs []*plumbing.Reference
	err = it.ForEach(func(ref *plumbing.Reference) error {
		if isCanonical(ref.Name()) {
			refs = append(refs, ref)
		}
		return nil
	})
	it.Close()
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// namespaced is a storage.Storer view exposing a single peer's copy of
// their refs — refs/namespaces/<ns>/* — with the namespace prefix
// stripped, go-git's equivalent of running git with GIT_NAMESPACE=<ns>
// set. Radicle namespaces contain no HEAD of their own, and the root HEAD
// is not under the namespace prefix, so this view never advertises HEAD —
// matching git-remote-rad's list::for_fetch namespaced branch. Objects,
// config, shallow and index storage pass straight through to the wrapped
// storer.
type namespaced struct {
	readOnlyRefs
	prefix string
}

// newNamespaced wraps base to expose refs/namespaces/<ns>/* with the
// prefix stripped.
func newNamespaced(base storage.Storer, ns string) *namespaced {
	return &namespaced{readOnlyRefs{Storer: base}, "refs/namespaces/" + ns + "/"}
}

// Reference returns the namespaced reference for name, which is given
// without the namespace prefix (e.g. "refs/heads/main").
func (n *namespaced) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	full, err := n.Storer.Reference(plumbing.ReferenceName(n.prefix + name.String()))
	if err != nil {
		return nil, err
	}
	return n.strip(full), nil
}

// IterReferences returns an iterator over refs/namespaces/<ns>/* with the
// namespace prefix stripped from each reference name.
func (n *namespaced) IterReferences() (storer.ReferenceIter, error) {
	it, err := n.Storer.IterReferences()
	if err != nil {
		return nil, err
	}

	var refs []*plumbing.Reference
	err = it.ForEach(func(ref *plumbing.Reference) error {
		if strings.HasPrefix(ref.Name().String(), n.prefix) {
			refs = append(refs, n.strip(ref))
		}
		return nil
	})
	it.Close()
	if err != nil {
		return nil, err
	}

	return storer.NewReferenceSliceIter(refs), nil
}

// strip removes the namespace prefix from ref's name (and, for a symbolic
// reference, from its target when the target is itself namespaced).
func (n *namespaced) strip(ref *plumbing.Reference) *plumbing.Reference {
	name := plumbing.ReferenceName(strings.TrimPrefix(ref.Name().String(), n.prefix))

	if ref.Type() == plumbing.SymbolicReference {
		target := ref.Target()
		target = plumbing.ReferenceName(strings.TrimPrefix(target.String(), n.prefix))
		return plumbing.NewSymbolicReference(name, target)
	}

	return plumbing.NewHashReference(name, ref.Hash())
}

var (
	_ storage.Storer = (*readOnlyRefs)(nil)
	_ storage.Storer = (*canonical)(nil)
	_ storage.Storer = (*namespaced)(nil)

	// The views must stay assertable as io.Closer: the composed file
	// transport releases its storer through that assertion.
	_ io.Closer = (*readOnlyRefs)(nil)
	_ io.Closer = (*canonical)(nil)
	_ io.Closer = (*namespaced)(nil)
)
