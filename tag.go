package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/core"
)

// Tag represents an annotated tag object. It points to a single git object of
// any type, but tags typically are applied to commit or blob objects. It
// provides a reference that associates the target with a tag name. It also
// contains meta-information about the tag, including the tagger, tag date and
// message.
//
// https://git-scm.com/book/en/v2/Git-Internals-Git-References#Tags
type Tag struct {
	Hash       core.Hash
	Name       string
	Tagger     Signature
	Message    string
	TargetType core.ObjectType
	Target     core.Hash

	r *Repository
}

// Type returns the type of object. It always returns core.TreeObject.
/*
func (t *Tag) Type() core.ObjectType {
	return core.TagObject
}
*/

// ID returns the object ID of the tag, not the object that the tag references.
// The returned value will always match the current value of Tag.Hash.
//
// ID is present to fulfill the Object interface.
func (t *Tag) ID() core.Hash {
	return t.Hash
}

// Type returns the type of object. It always returns core.TagObject.
//
// Type is present to fulfill the Object interface.
func (t *Tag) Type() core.ObjectType {
	return core.TagObject
}

// Decode transforms a core.Object into a Tag struct.
func (t *Tag) Decode(o core.Object) (err error) {
	if o.Type() != core.TagObject {
		return ErrUnsupportedObject
	}

	t.Hash = o.Hash()

	reader, err := o.Reader()
	if err != nil {
		return err
	}
	defer checkClose(reader, &err)

	r := bufio.NewReader(reader)
	for {
		line, err := r.ReadSlice('\n')
		if err != nil && err != io.EOF {
			return err
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			break // Start of message
		}

		split := bytes.SplitN(line, []byte{' '}, 2)
		switch string(split[0]) {
		case "object":
			t.Target = core.NewHash(string(split[1]))
		case "type":
			t.TargetType, err = core.ParseObjectType(string(split[1]))
			if err != nil {
				return err
			}
		case "tag":
			t.Name = string(split[1])
		case "tagger":
			t.Tagger.Decode(split[1])
		}

		if err == io.EOF {
			return nil
		}
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	t.Message = string(data)

	return nil
}

// Encode transforms a Tag into a core.Object.
func (t *Tag) Encode(o core.Object) error {
	o.SetType(core.TagObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}
	defer checkClose(w, &err)

	if _, err = fmt.Fprintf(w,
		"object %s\ntype %s\ntag %s\ntagger ",
		t.Target.String(), t.TargetType.Bytes(), t.Name); err != nil {
		return err
	}

	if err = t.Tagger.Encode(w); err != nil {
		return err
	}

	if _, err = fmt.Fprint(w, "\n\n"); err != nil {
		return err
	}

	if _, err = fmt.Fprint(w, t.Message); err != nil {
		return err
	}

	return err
}

// Commit returns the commit pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Commit() (*Commit, error) {
	if t.TargetType != core.CommitObject {
		return nil, ErrUnsupportedObject
	}
	return t.r.Commit(t.Target)
}

// Tree returns the tree pointed to by the tag. If the tag points to a commit
// object the tree of that commit will be returned. If the tag does not point
// to a commit or tree object ErrUnsupportedObject will be returned.
func (t *Tag) Tree() (*Tree, error) {
	switch t.TargetType {
	case core.CommitObject:
		commit, err := t.r.Commit(t.Target)
		if err != nil {
			return nil, err
		}
		return commit.Tree()
	case core.TreeObject:
		return t.r.Tree(t.Target)
	default:
		return nil, ErrUnsupportedObject
	}
}

// Blob returns the blob pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Blob() (*Blob, error) {
	if t.TargetType != core.BlobObject {
		return nil, ErrUnsupportedObject
	}
	return t.r.Blob(t.Target)
}

// Object returns the object pointed to by the tag.
func (t *Tag) Object() (Object, error) {
	return t.r.Object(t.TargetType, t.Target)
}

// String returns the meta information contained in the tag as a formatted
// string.
func (t *Tag) String() string {
	obj, _ := t.Object()
	return fmt.Sprintf(
		"%s %s\nTagger: %s\nDate:   %s\n\n%s\n%s",
		core.TagObject, t.Name, t.Tagger.String(), t.Tagger.When.Format(DateFormat),
		t.Message, obj,
	)
}

// TagIter provides an iterator for a set of tags.
type TagIter struct {
	core.ObjectIter
	r *Repository
}

// NewTagIter returns a TagIter for the given repository and underlying
// object iterator.
//
// The returned TagIter will automatically skip over non-tag objects.
func NewTagIter(r *Repository, iter core.ObjectIter) *TagIter {
	return &TagIter{iter, r}
}

// Next moves the iterator to the next tag and returns a pointer to it. If it
// has reached the end of the set it will return io.EOF.
func (iter *TagIter) Next() (*Tag, error) {
	obj, err := iter.ObjectIter.Next()
	if err != nil {
		return nil, err
	}

	tag := &Tag{r: iter.r}
	return tag, tag.Decode(obj)
}

// ForEach call the cb function for each tag contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *TagIter) ForEach(cb func(*Tag) error) error {
	return iter.ObjectIter.ForEach(func(obj core.Object) error {
		tag := &Tag{r: iter.r}
		if err := tag.Decode(obj); err != nil {
			return err
		}

		return cb(tag)
	})
}
