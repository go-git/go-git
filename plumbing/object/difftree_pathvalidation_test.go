package object

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

type DiffTreePathValidationSuite struct{}

var _ = Suite(&DiffTreePathValidationSuite{})

func (s *DiffTreePathValidationSuite) storeBlob(c *C, sto *memory.Storage, content string) plumbing.Hash {
	obj := sto.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)

	w, err := obj.Writer()
	c.Assert(err, IsNil)
	_, err = w.Write([]byte(content))
	c.Assert(err, IsNil)
	c.Assert(w.Close(), IsNil)

	h, err := sto.SetEncodedObject(obj)
	c.Assert(err, IsNil)
	return h
}

// storeTree writes a tree verbatim. Tree.Encode rejects only NUL, matching
// upstream Git, which forbids just '/' and NUL inside a path component.
func (s *DiffTreePathValidationSuite) storeTree(c *C, sto *memory.Storage, entries []TreeEntry) *Tree {
	tree := &Tree{Entries: entries}

	obj := sto.NewEncodedObject()
	c.Assert(tree.Encode(obj), IsNil)

	h, err := sto.SetEncodedObject(obj)
	c.Assert(err, IsNil)

	stored, err := GetTree(sto, h)
	c.Assert(err, IsNil)
	return stored
}

// TestDiffTree_ControlCharacterEntry checks that a tree entry name upstream
// Git accepts does not abort a diff. DiffTree computes changes in memory and
// never writes a name to disk, so the worktree-safety validation in
// TreeWalker.Next does not apply to it.
//
// Before the fix this failed with:
//
//	from: invalid path "\x1b\x1b\x1b": contains control character
func (s *DiffTreePathValidationSuite) TestDiffTree_ControlCharacterEntry(c *C) {
	sto := memory.NewStorage()

	before := s.storeBlob(c, sto, "one")
	after := s.storeBlob(c, sto, "two")

	from := s.storeTree(c, sto, []TreeEntry{
		{Name: "\x1b\x1b\x1b", Mode: filemode.Regular, Hash: before},
	})
	to := s.storeTree(c, sto, []TreeEntry{
		{Name: "\x1b\x1b\x1b", Mode: filemode.Regular, Hash: after},
	})

	changes, err := DiffTree(from, to)
	c.Assert(err, IsNil)
	c.Assert(changes, HasLen, 1)

	action, err := changes[0].Action()
	c.Assert(err, IsNil)
	c.Assert(action.String(), Equals, "Modify")
}
