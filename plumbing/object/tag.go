package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
	"github.com/go-git/go-git/v6/x/plugin"
)

// ErrMalformedTag is returned when a tag object cannot be decoded because
// its required headers (object, type, tag) are missing or out of order.
var ErrMalformedTag = errors.New("malformed tag")

// Tag represents an annotated tag object. It points to a single git object of
// any type, but tags typically are applied to commit or blob objects. It
// provides a reference that associates the target with a tag name. It also
// contains meta-information about the tag, including the tagger, tag date and
// message.
//
// Note that this is not used for lightweight tags.
//
// https://git-scm.com/book/en/v2/Git-Internals-Git-References#Tags
type Tag struct {
	// Hash of the tag.
	Hash plumbing.Hash
	// Name of the tag.
	Name string
	// Tagger is the one who created the tag.
	Tagger Signature
	// Message is an arbitrary text message.
	Message string
	// Signature is the cryptographic signature appended after the message
	// body. This is the canonical tag signature in upstream Git.
	Signature []byte
	// SignatureSHA256 is the cryptographic signature stored under the
	// "gpgsig-sha256" header.
	SignatureSHA256 []byte
	// TargetType is the object type of the target.
	TargetType plumbing.ObjectType
	// Target is the hash of the target object.
	Target plumbing.Hash

	s storer.EncodedObjectStorer
	// src holds the encoded object this Tag was decoded from, used by
	// EncodeWithoutSignature to recover the canonical signed bytes.
	src plumbing.EncodedObject
}

// GetTag gets a tag from an object storer and decodes it.
func GetTag(s storer.EncodedObjectStorer, h plumbing.Hash) (*Tag, error) {
	o, err := s.EncodedObject(plumbing.TagObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeTag(s, o)
}

// DecodeTag decodes an encoded object into a *Commit and associates it to the
// given object storer.
func DecodeTag(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (*Tag, error) {
	t := &Tag{s: s}
	if err := t.Decode(o); err != nil {
		return nil, err
	}

	return t, nil
}

// ID returns the object ID of the tag, not the object that the tag references.
// The returned value will always match the current value of Tag.Hash.
//
// ID is present to fulfill the Object interface.
func (t *Tag) ID() plumbing.Hash {
	return t.Hash
}

// Type returns the type of object. It always returns plumbing.TagObject.
//
// Type is present to fulfill the Object interface.
func (t *Tag) Type() plumbing.ObjectType {
	return plumbing.TagObject
}

func (t *Tag) reset() {
	storer := t.s
	*t = Tag{s: storer}
}

// Decode transforms a plumbing.EncodedObject into a Tag struct.
func (t *Tag) Decode(o plumbing.EncodedObject) (err error) {
	if o.Type() != plumbing.TagObject {
		return ErrUnsupportedObject
	}

	t.reset()
	t.Hash = o.Hash()
	t.src = o

	reader, err := o.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(reader, &err)

	r := sync.GetBufioReader(reader)
	defer sync.PutBufioReader(r)

	s := &tagScanner{r: r, t: t}
	for state := scanTagObject; state != nil; {
		state, err = state(s)
		if err != nil {
			return err
		}
	}

	data := s.msgbuf.Bytes()
	if sm := parseSignedBytes(data); sm >= 0 {
		t.Signature = bytes.Clone(data[sm:])
		data = data[:sm]
	}
	t.Message = string(data)

	return nil
}

// Encode transforms a Tag into a plumbing.EncodedObject.
func (t *Tag) Encode(o plumbing.EncodedObject) error {
	return t.encode(o, true)
}

// EncodeWithoutSignature returns a reader over the Tag's bytes without any
// signature data, i.e. the payload that signatures are computed over.
//
// Behaviour mirrors Commit.EncodeWithoutSignature:
//
//   - For Tags populated by Decode whose exported fields still match the
//     source object, the payload is streamed from the raw source bytes with
//     the inline trailing signature truncated and gpgsig/gpgsig-sha256
//     headers (and their continuation lines) stripped verbatim. This
//     preserves the exact bytes the signature was computed over, regardless
//     of any normalization performed by Decode.
//
//   - For Tags constructed in memory, or for decoded Tags whose exported
//     fields have been mutated, the payload is derived from the current
//     struct fields. Mutation is detected by re-decoding the source object
//     and comparing exported fields; if any differ, the in-memory
//     representation prevails.
func (t *Tag) EncodeWithoutSignature() (io.Reader, error) {
	if t.matchesSource() {
		return t.sourcePayload(), nil
	}
	return &signedReader{writeTo: func(w io.Writer) error {
		return t.encodeTo(w, false)
	}}, nil
}

// sourcePayload returns a reader over the source object's signature-stripped
// bytes. See Commit.sourcePayload. The caller must ensure t.src is set.
func (t *Tag) sourcePayload() io.Reader {
	src := t.src
	return &signedReader{writeTo: func(w io.Writer) error {
		return stripObjectSignatures(w, src, plumbing.TagObject)
	}}
}

// matchesSource reports whether t.src is set and re-decoding it produces a
// Tag whose payload-affecting exported fields are identical to those of t.
//
// Signature and SignatureSHA256 are intentionally excluded from the
// comparison: neither path emits them as part of the verification payload,
// so mutating them must not trigger a switch to struct-encode (which would
// change the byte layout the caller is trying to verify against).
func (t *Tag) matchesSource() bool {
	if t.src == nil {
		return false
	}
	fresh := &Tag{}
	if err := fresh.Decode(t.src); err != nil {
		return false
	}
	return t.Hash == fresh.Hash &&
		t.Name == fresh.Name &&
		signatureEqual(t.Tagger, fresh.Tagger) &&
		t.Message == fresh.Message &&
		t.TargetType == fresh.TargetType &&
		t.Target == fresh.Target
}

func (t *Tag) encode(o plumbing.EncodedObject, includeSig bool) (err error) {
	o.SetType(plumbing.TagObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(w, &err)

	return t.encodeTo(w, includeSig)
}

// encodeTo writes the tag's canonical bytes to w, including the signature only
// when includeSig is true.
func (t *Tag) encodeTo(w io.Writer, includeSig bool) (err error) {
	if _, err = fmt.Fprintf(w,
		"object %s\ntype %s\ntag %s\n",
		t.Target.String(), t.TargetType.Bytes(), t.Name); err != nil {
		return err
	}

	if !isZeroSignature(t.Tagger) {
		if _, err = fmt.Fprint(w, "tagger "); err != nil {
			return err
		}

		if err = t.Tagger.Encode(w); err != nil {
			return err
		}

		if _, err = fmt.Fprint(w, "\n"); err != nil {
			return err
		}
	}

	// gpgsig-sha256 is emitted between the tagger line and the blank line
	// that separates headers from the body, matching upstream's
	// add_header_signature insertion point (commit.c:1142-1171), which
	// builtin/tag.c:do_sign reuses when signing tags in compat mode.
	if len(t.SignatureSHA256) > 0 && includeSig {
		if _, err = fmt.Fprint(w, headerpgp256+" "); err != nil {
			return err
		}
		if _, err = w.Write(indentSignature(t.SignatureSHA256)); err != nil {
			return err
		}
		if _, err = fmt.Fprint(w, "\n"); err != nil {
			return err
		}
	}

	if _, err = io.WriteString(w, "\n"); err != nil {
		return err
	}

	// Write the message via io.WriteString rather than fmt: fmt copies the
	// whole (potentially large) message into an internal buffer, whereas
	// io.WriteString streams it straight to a StringWriter sink.
	if _, err = io.WriteString(w, t.Message); err != nil {
		return err
	}

	// Note that this is highly sensitive to what is sent along in the
	// message. Message *always* needs to end with a newline, or else the
	// message and the trailing signature will be concatenated into a
	// corrupt object. Since this is a lower-level method, we assume you
	// know what you are doing and have already done the needful on the
	// message in the caller.
	if includeSig {
		if _, err = w.Write(t.Signature); err != nil {
			return err
		}
	}

	return err
}

func isZeroSignature(s Signature) bool {
	return s.Name == "" && s.Email == "" && s.When.IsZero()
}

// Commit returns the commit pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Commit() (*Commit, error) {
	if t.TargetType != plumbing.CommitObject {
		return nil, ErrUnsupportedObject
	}

	o, err := t.s.EncodedObject(plumbing.CommitObject, t.Target)
	if err != nil {
		return nil, err
	}

	return DecodeCommit(t.s, o)
}

// Tree returns the tree pointed to by the tag. If the tag points to a commit
// object the tree of that commit will be returned. If the tag does not point
// to a commit or tree object ErrUnsupportedObject will be returned.
func (t *Tag) Tree() (*Tree, error) {
	switch t.TargetType {
	case plumbing.CommitObject:
		c, err := t.Commit()
		if err != nil {
			return nil, err
		}

		return c.Tree()
	case plumbing.TreeObject:
		return GetTree(t.s, t.Target)
	default:
		return nil, ErrUnsupportedObject
	}
}

// Blob returns the blob pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Blob() (*Blob, error) {
	if t.TargetType != plumbing.BlobObject {
		return nil, ErrUnsupportedObject
	}

	return GetBlob(t.s, t.Target)
}

// Object returns the object pointed to by the tag.
func (t *Tag) Object() (Object, error) {
	o, err := t.s.EncodedObject(t.TargetType, t.Target)
	if err != nil {
		return nil, err
	}

	return DecodeObject(t.s, o)
}

// String returns the meta information contained in the tag as a formatted
// string.
func (t *Tag) String() string {
	obj, _ := t.Object()

	return fmt.Sprintf(
		"%s %s\nTagger: %s\nDate:   %s\n\n%s\n%s",
		plumbing.TagObject, t.Name, t.Tagger.String(), t.Tagger.When.Format(DateFormat),
		t.Message, objectAsString(obj),
	)
}

// Verify checks the signature of the tag using the Verifier provided via opts,
// or, when none is given, the verifier registered through
// plugin.ObjectVerifier. It returns ErrNotSigned when the tag carries no
// signature.
func (t *Tag) Verify(ctx context.Context, opts ...VerifyOption) (*plugin.Verification, error) {
	payload, err := t.EncodeWithoutSignature()
	if err != nil {
		return nil, err
	}
	return Verify(ctx, payload, t.Signature, opts...)
}

// TagIter provides an iterator for a set of tags.
type TagIter struct {
	storer.EncodedObjectIter
	s storer.EncodedObjectStorer
}

// NewTagIter takes a storer.EncodedObjectStorer and a
// storer.EncodedObjectIter and returns a *TagIter that iterates over all
// tags contained in the storer.EncodedObjectIter.
//
// Any non-tag object returned by the storer.EncodedObjectIter is skipped.
func NewTagIter(s storer.EncodedObjectStorer, iter storer.EncodedObjectIter) *TagIter {
	return &TagIter{iter, s}
}

// Next moves the iterator to the next tag and returns a pointer to it. If
// there are no more tags, it returns io.EOF.
func (iter *TagIter) Next() (*Tag, error) {
	obj, err := iter.EncodedObjectIter.Next()
	if err != nil {
		return nil, err
	}

	return DecodeTag(iter.s, obj)
}

// ForEach call the cb function for each tag contained on this iter until
// an error happens or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *TagIter) ForEach(cb func(*Tag) error) error {
	return iter.EncodedObjectIter.ForEach(func(obj plumbing.EncodedObject) error {
		t, err := DecodeTag(iter.s, obj)
		if err != nil {
			return err
		}

		return cb(t)
	})
}

func objectAsString(obj Object) string {
	switch o := obj.(type) {
	case *Commit:
		return o.String()
	default:
		return ""
	}
}
