package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v3/core"
)

// Tag represents an annotated tag object. It points to a single git object of
// any type, but tags typically are applied to commit or blob objects. It
// provides a reference that associates the target with a tag name. It also
// contains meta-information about the tag, including the tagger, tag date and
// message.
//
// https://git-scm.com/book/en/v2/Git-Internals-Git-References#Tags
type Tag struct {
	Hash    core.Hash
	Type    core.ObjectType
	Name    string
	Tagger  Signature
	Message string

	object core.Hash
	r      *Repository
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
	defer close(reader, &err)

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
			t.object = core.NewHash(string(split[1]))
		case "type":
			t.Type, err = core.ParseObjectType(string(split[1]))
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

// Commit returns the commit pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Commit() (*Commit, error) {
	if t.Type != core.CommitObject {
		return nil, ErrUnsupportedObject
	}
	return t.r.Commit(t.object)
}

// Tree returns the tree pointed to by the tag. If the tag points to a commit
// object the tree of that commit will be returned. If the tag does not point
// to a commit or tree object ErrUnsupportedObject will be returned.
func (t *Tag) Tree() (*Tree, error) {
	// TODO: If the tag is of type commit, follow the commit to its tree?
	switch t.Type {
	case core.CommitObject:
		commit, err := t.r.Commit(t.object)
		if err != nil {
			return nil, err
		}
		return commit.Tree(), nil
	case core.TreeObject:
		return t.r.Tree(t.object)
	default:
		return nil, ErrUnsupportedObject
	}
}

// Blob returns the blob pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Blob() (*Blob, error) {
	if t.Type != core.BlobObject {
		return nil, ErrUnsupportedObject
	}
	return t.r.Blob(t.object)
}

// Object returns the object pointed to by the tag.
func (t *Tag) Object() (core.Object, error) {
	return t.r.Storage.Get(t.object)
}

// String returns the meta information contained in the tag as a formatted
// string.
func (t *Tag) String() string {
	return fmt.Sprintf(
		"%s %s\nObject: %s\nType: %s\nTag: %s\nTagger: %s\nDate:   %s\n",
		core.TagObject, t.Hash, t.object, t.Type, t.Name, t.Tagger.String(), t.Tagger.When,
	)
}

// TagIter provides an iterator for a set of tags.
type TagIter struct {
	core.ObjectIter
	r *Repository
}

// NewTagIter returns a new TagIter for the given Repository and ObjectIter.
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

// Close releases any resources used by the iterator.
func (iter *TagIter) Close() {
	iter.Close()
}
