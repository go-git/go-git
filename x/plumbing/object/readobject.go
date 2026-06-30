// Package object provides immutable, read-only views of Git commit and tag
// objects decoded from a store.
//
// A ReadCommit or ReadTag exposes its fields only through accessor methods, and
// slice-valued accessors return copies, so the view cannot be mutated after
// construction. That immutability is what lets Verify reproduce the signed
// payload directly from the stored source bytes — via object.SignedPayload —
// without re-checking the object against its source the way the mutable
// object.Commit / object.Tag must. Use these types when you decode an object
// solely to inspect or verify it; use object.Commit / object.Tag when you
// intend to modify and re-encode it.
package object

import (
	"bytes"
	"context"
	"slices"

	"github.com/go-git/go-git/v6/plumbing"
	gitobject "github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/x/plugin"
)

// ReadCommit is an immutable, read-only view of a commit decoded from an object
// store.
type ReadCommit struct {
	c   *gitobject.Commit
	src plumbing.EncodedObject
}

// GetReadCommit gets a commit from an object storer and returns it as an
// immutable ReadCommit.
func GetReadCommit(s storer.EncodedObjectStorer, h plumbing.Hash) (*ReadCommit, error) {
	o, err := s.EncodedObject(plumbing.CommitObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeReadCommit(s, o)
}

// DecodeReadCommit decodes an encoded object into an immutable ReadCommit and
// associates it to the given object storer.
func DecodeReadCommit(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (*ReadCommit, error) {
	c, err := gitobject.DecodeCommit(s, o)
	if err != nil {
		return nil, err
	}

	return &ReadCommit{c: c, src: o}, nil
}

// Hash returns the object ID of the commit.
func (rc *ReadCommit) Hash() plumbing.Hash { return rc.c.Hash }

// Author returns the original author of the commit.
func (rc *ReadCommit) Author() gitobject.Signature { return rc.c.Author }

// Committer returns the committer of the commit.
func (rc *ReadCommit) Committer() gitobject.Signature { return rc.c.Committer }

// Message returns the commit message.
func (rc *ReadCommit) Message() string { return rc.c.Message }

// TreeHash returns the hash of the root tree of the commit.
func (rc *ReadCommit) TreeHash() plumbing.Hash { return rc.c.TreeHash }

// Encoding returns the encoding of the commit message.
func (rc *ReadCommit) Encoding() gitobject.MessageEncoding { return rc.c.Encoding }

// ParentHashes returns the hashes of the parent commits. The returned slice is
// a copy; mutating it does not affect the ReadCommit.
func (rc *ReadCommit) ParentHashes() []plumbing.Hash { return slices.Clone(rc.c.ParentHashes) }

// ExtraHeaders returns the non-standard headers of the commit. The returned
// slice is a copy; mutating it does not affect the ReadCommit.
func (rc *ReadCommit) ExtraHeaders() []gitobject.ExtraHeader { return slices.Clone(rc.c.ExtraHeaders) }

// Signature returns the embedded cryptographic signature. The returned slice is
// a copy; mutating it does not affect the ReadCommit.
func (rc *ReadCommit) Signature() []byte { return bytes.Clone(rc.c.Signature) }

// SignatureSHA256 returns the SHA-256 cryptographic signature. The returned
// slice is a copy; mutating it does not affect the ReadCommit.
func (rc *ReadCommit) SignatureSHA256() []byte { return bytes.Clone(rc.c.SignatureSHA256) }

// Verify checks the signature of the commit using the Verifier provided via
// opts, or, when none is given, the verifier registered through
// plugin.ObjectVerifier. It returns object.ErrNotSigned when the commit carries
// no signature.
//
// Because a ReadCommit is immutable, verification reproduces the signed payload
// straight from the stored source bytes.
func (rc *ReadCommit) Verify(ctx context.Context, opts ...gitobject.VerifyOption) (*plugin.Verification, error) {
	payload, err := gitobject.SignedPayload(rc.src)
	if err != nil {
		return nil, err
	}

	return gitobject.Verify(ctx, payload, rc.c.Signature, opts...)
}

// ReadTag is an immutable, read-only view of an annotated tag decoded from an
// object store. It mirrors ReadCommit.
type ReadTag struct {
	t   *gitobject.Tag
	src plumbing.EncodedObject
}

// GetReadTag gets a tag from an object storer and returns it as an immutable
// ReadTag.
func GetReadTag(s storer.EncodedObjectStorer, h plumbing.Hash) (*ReadTag, error) {
	o, err := s.EncodedObject(plumbing.TagObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeReadTag(s, o)
}

// DecodeReadTag decodes an encoded object into an immutable ReadTag and
// associates it to the given object storer.
func DecodeReadTag(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (*ReadTag, error) {
	t, err := gitobject.DecodeTag(s, o)
	if err != nil {
		return nil, err
	}

	return &ReadTag{t: t, src: o}, nil
}

// Hash returns the object ID of the tag.
func (rt *ReadTag) Hash() plumbing.Hash { return rt.t.Hash }

// Name returns the tag name.
func (rt *ReadTag) Name() string { return rt.t.Name }

// Tagger returns the identity that created the tag.
func (rt *ReadTag) Tagger() gitobject.Signature { return rt.t.Tagger }

// Message returns the tag message.
func (rt *ReadTag) Message() string { return rt.t.Message }

// TargetType returns the object type of the tag's target.
func (rt *ReadTag) TargetType() plumbing.ObjectType { return rt.t.TargetType }

// Target returns the hash of the tag's target object.
func (rt *ReadTag) Target() plumbing.Hash { return rt.t.Target }

// Signature returns the embedded cryptographic signature. The returned slice is
// a copy; mutating it does not affect the ReadTag.
func (rt *ReadTag) Signature() []byte { return bytes.Clone(rt.t.Signature) }

// SignatureSHA256 returns the SHA-256 cryptographic signature. The returned
// slice is a copy; mutating it does not affect the ReadTag.
func (rt *ReadTag) SignatureSHA256() []byte { return bytes.Clone(rt.t.SignatureSHA256) }

// Verify checks the signature of the tag using the Verifier provided via opts,
// or, when none is given, the verifier registered through plugin.ObjectVerifier.
// It returns object.ErrNotSigned when the tag carries no signature.
//
// Because a ReadTag is immutable, verification reproduces the signed payload
// straight from the stored source bytes.
func (rt *ReadTag) Verify(ctx context.Context, opts ...gitobject.VerifyOption) (*plugin.Verification, error) {
	payload, err := gitobject.SignedPayload(rt.src)
	if err != nil {
		return nil, err
	}

	return gitobject.Verify(ctx, payload, rt.t.Signature, opts...)
}
