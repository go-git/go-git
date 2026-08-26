// Package object contains implementations of all Git objects and utility
// functions to work with them.
package object

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ErrUnsupportedObject trigger when a non-supported object is being decoded.
var ErrUnsupportedObject = errors.New("unsupported object type")

// Object is a generic representation of any git object. It is implemented by
// Commit, Tree, Blob, and Tag, and includes the functions that are common to
// them.
//
// Object is returned when an object can be of any type. It is frequently used
// with a type cast to acquire the specific type of object:
//
//	func process(obj Object) {
//		switch o := obj.(type) {
//		case *Commit:
//			// o is a Commit
//		case *Tree:
//			// o is a Tree
//		case *Blob:
//			// o is a Blob
//		case *Tag:
//			// o is a Tag
//		}
//	}
//
// This interface is intentionally different from plumbing.EncodedObject, which
// is a lower level interface used by storage implementations to read and write
// objects in its encoded form.
type Object interface {
	ID() plumbing.Hash
	Type() plumbing.ObjectType
	Decode(plumbing.EncodedObject) error
	Encode(plumbing.EncodedObject) error
}

// GetObject gets an object from an object storer and decodes it.
func GetObject(s storer.EncodedObjectStorer, h plumbing.Hash) (Object, error) {
	o, err := s.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeObject(s, o)
}

// DecodeObject decodes an encoded object into an Object and associates it to
// the given object storer.
func DecodeObject(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (Object, error) {
	switch o.Type() {
	case plumbing.CommitObject:
		return DecodeCommit(s, o)
	case plumbing.TreeObject:
		return DecodeTree(s, o)
	case plumbing.BlobObject:
		return DecodeBlob(o)
	case plumbing.TagObject:
		return DecodeTag(s, o)
	default:
		return nil, plumbing.ErrInvalidType
	}
}

// DateFormat is the format being used in the original git implementation
const DateFormat = "Mon Jan 02 15:04:05 2006 -0700"

// Signature is used to identify who and when created a commit or tag.
type Signature struct {
	// Name represents a person name. It is an arbitrary string.
	Name string
	// Email is an email, but it cannot be assumed to be well-formed.
	Email string
	// When is the timestamp of the signature.
	When time.Time
}

// Decode decodes a byte slice into a signature
func (s *Signature) Decode(b []byte) {
	s.decode(string(b))
}

func (s *Signature) decode(raw string) {
	open := strings.LastIndexByte(raw, '<')
	closeBracket := strings.LastIndexByte(raw, '>')
	if open == -1 || closeBracket == -1 {
		return
	}

	if closeBracket < open {
		return
	}

	s.Name = strings.Trim(raw[:open], " ")
	s.Email = raw[open+1 : closeBracket]

	timeAndTimeZone := strings.TrimLeft(raw[closeBracket+1:], " \t\n\v\f\r")
	if len(timeAndTimeZone) > 0 {
		s.decodeTimeAndTimeZone(timeAndTimeZone)
	}
}

// Encode encodes a Signature into a writer.
func (s *Signature) Encode(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s <%s> ", s.Name, s.Email); err != nil {
		return err
	}
	if err := s.encodeTimeAndTimeZone(w); err != nil {
		return err
	}
	return nil
}

// identSource keeps the exact header value and the signature decoded from it.
// The raw value is reusable only while the public Signature is unchanged.
type identSource struct {
	raw       string
	signature Signature
	present   bool
}

func newIdentSource(raw []byte) identSource {
	source := identSource{raw: string(raw), present: true}
	source.signature.decode(source.raw)
	return source
}

func (s identSource) matches(signature Signature) bool {
	return s.present && signatureEqual(s.signature, signature)
}

func (s identSource) encode(w io.Writer, signature Signature) error {
	if s.matches(signature) {
		_, err := io.WriteString(w, s.raw)
		return err
	}
	return signature.Encode(w)
}

func signatureEqual(a, b Signature) bool {
	return a.Name == b.Name &&
		a.Email == b.Email &&
		a.When.Unix() == b.When.Unix() &&
		a.When.Format("-0700") == b.When.Format("-0700")
}

var timeZoneLength = 5

func (s *Signature) decodeTimeAndTimeZone(raw string) {
	space := strings.IndexByte(raw, ' ')
	if space == -1 {
		space = len(raw)
	}

	ts, err := strconv.ParseInt(raw[:space], 10, 64)
	if err != nil {
		return
	}

	s.When = time.Unix(ts, 0).In(time.UTC)
	tzStart := space + 1
	if tzStart >= len(raw) || tzStart+timeZoneLength > len(raw) {
		return
	}

	timezone := raw[tzStart : tzStart+timeZoneLength]
	tzhours, err1 := strconv.ParseInt(timezone[0:3], 10, 64)
	tzmins, err2 := strconv.ParseInt(timezone[3:], 10, 64)
	if err1 != nil || err2 != nil {
		return
	}
	if tzhours < 0 {
		tzmins *= -1
	}

	tz := time.FixedZone("", int(tzhours*60*60+tzmins*60))

	s.When = s.When.In(tz)
}

func (s *Signature) encodeTimeAndTimeZone(w io.Writer) error {
	u := max(s.When.Unix(), 0)
	_, err := fmt.Fprintf(w, "%d %s", u, s.When.Format("-0700"))
	return err
}

func (s *Signature) String() string {
	return fmt.Sprintf("%s <%s>", s.Name, s.Email)
}

// ObjectIter provides an iterator for a set of objects.
type ObjectIter struct { //nolint:revive // stutters but is a well-established name
	storer.EncodedObjectIter
	s storer.EncodedObjectStorer
}

// NewObjectIter takes a storer.EncodedObjectStorer and a
// storer.EncodedObjectIter and returns an *ObjectIter that iterates over all
// objects contained in the storer.EncodedObjectIter.
func NewObjectIter(s storer.EncodedObjectStorer, iter storer.EncodedObjectIter) *ObjectIter {
	return &ObjectIter{iter, s}
}

// Next moves the iterator to the next object and returns a pointer to it. If
// there are no more objects, it returns io.EOF.
func (iter *ObjectIter) Next() (Object, error) {
	for {
		obj, err := iter.EncodedObjectIter.Next()
		if err != nil {
			return nil, err
		}

		o, err := iter.toObject(obj)
		if errors.Is(err, plumbing.ErrInvalidType) {
			continue
		}

		if err != nil {
			return nil, err
		}

		return o, nil
	}
}

// ForEach call the cb function for each object contained on this iter until
// an error happens or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *ObjectIter) ForEach(cb func(Object) error) error {
	return iter.EncodedObjectIter.ForEach(func(obj plumbing.EncodedObject) error {
		o, err := iter.toObject(obj)
		if errors.Is(err, plumbing.ErrInvalidType) {
			return nil
		}

		if err != nil {
			return err
		}

		return cb(o)
	})
}

func (iter *ObjectIter) toObject(obj plumbing.EncodedObject) (Object, error) {
	switch obj.Type() {
	case plumbing.BlobObject:
		blob := &Blob{}
		return blob, blob.Decode(obj)
	case plumbing.TreeObject:
		tree := &Tree{s: iter.s}
		return tree, tree.Decode(obj)
	case plumbing.CommitObject:
		commit := &Commit{}
		return commit, commit.Decode(obj)
	case plumbing.TagObject:
		tag := &Tag{}
		return tag, tag.Decode(obj)
	default:
		return nil, plumbing.ErrInvalidType
	}
}
