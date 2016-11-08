package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// ErrUnsupportedObject trigger when a non-supported object is being decoded.
var ErrUnsupportedObject = errors.New("unsupported object type")

// Object is a generic representation of any git object. It is implemented by
// Commit, Tree, Blob and Tag, and includes the functions that are common to
// them.
//
// Object is returned when an object could of any type. It is frequently used
// with a type cast to acquire the specific type of object:
//
//   func process(obj Object) {
//   	switch o := obj.(type) {
//   	case *Commit:
//   		// o is a Commit
//   	case *Tree:
//   		// o is a Tree
//   	case *Blob:
//   		// o is a Blob
//   	case *Tag:
//   		// o is a Tag
//   	}
//   }
//
// This interface is intentionally different from plumbing.Object, which is a lower
// level interface used by storage implementations to read and write objects.
type Object interface {
	ID() plumbing.Hash
	Type() plumbing.ObjectType
	Decode(plumbing.Object) error
	Encode(plumbing.Object) error
}

// Signature represents an action signed by a person
type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// Decode decodes a byte slice into a signature
func (s *Signature) Decode(b []byte) {
	open := bytes.IndexByte(b, '<')
	close := bytes.IndexByte(b, '>')
	if open == -1 || close == -1 {
		return
	}

	s.Name = string(bytes.Trim(b[:open], " "))
	s.Email = string(b[open+1 : close])

	hasTime := close+2 < len(b)
	if hasTime {
		s.decodeTimeAndTimeZone(b[close+2:])
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

var timeZoneLength = 5

func (s *Signature) decodeTimeAndTimeZone(b []byte) {
	space := bytes.IndexByte(b, ' ')
	if space == -1 {
		space = len(b)
	}

	ts, err := strconv.ParseInt(string(b[:space]), 10, 64)
	if err != nil {
		return
	}

	s.When = time.Unix(ts, 0).In(time.UTC)
	var tzStart = space + 1
	if tzStart >= len(b) || tzStart+timeZoneLength > len(b) {
		return
	}

	tl, err := time.Parse("-0700", string(b[tzStart:tzStart+timeZoneLength]))
	if err != nil {
		return
	}

	s.When = s.When.In(tl.Location())
}

func (s *Signature) encodeTimeAndTimeZone(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%d %s", s.When.Unix(), s.When.Format("-0700"))
	return err
}

func (s *Signature) String() string {
	return fmt.Sprintf("%s <%s>", s.Name, s.Email)
}

// ObjectIter provides an iterator for a set of objects.
type ObjectIter struct {
	storer.ObjectIter
	r *Repository
}

// NewObjectIter returns a ObjectIter for the given repository and underlying
// object iterator.
func NewObjectIter(r *Repository, iter storer.ObjectIter) *ObjectIter {
	return &ObjectIter{iter, r}
}

// Next moves the iterator to the next object and returns a pointer to it. If it
// has reached the end of the set it will return io.EOF.
func (iter *ObjectIter) Next() (Object, error) {
	for {
		obj, err := iter.ObjectIter.Next()
		if err != nil {
			return nil, err
		}

		o, err := iter.toObject(obj)
		if err == plumbing.ErrInvalidType {
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
	return iter.ObjectIter.ForEach(func(obj plumbing.Object) error {
		o, err := iter.toObject(obj)
		if err == plumbing.ErrInvalidType {
			return nil
		}

		if err != nil {
			return err
		}

		return cb(o)
	})
}

func (iter *ObjectIter) toObject(obj plumbing.Object) (Object, error) {
	switch obj.Type() {
	case plumbing.BlobObject:
		blob := &Blob{}
		return blob, blob.Decode(obj)
	case plumbing.TreeObject:
		tree := &Tree{r: iter.r}
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
