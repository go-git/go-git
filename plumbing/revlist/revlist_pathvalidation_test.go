package revlist

import (
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

// storeBlob writes a blob and returns its hash.
func storeBlob(c *C, sto *memory.Storage, content string) plumbing.Hash {
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

// storeTree writes a tree verbatim, bypassing the higher-level helpers so
// that entry names Git itself accepts can be stored. Tree.Encode only
// rejects NUL, matching upstream Git, which forbids just '/' and NUL inside
// a path component.
func storeTree(c *C, sto *memory.Storage, entries []object.TreeEntry) plumbing.Hash {
	tree := &object.Tree{Entries: entries}

	obj := sto.NewEncodedObject()
	c.Assert(tree.Encode(obj), IsNil)

	h, err := sto.SetEncodedObject(obj)
	c.Assert(err, IsNil)
	return h
}

// storeCommit writes a commit pointing at treeHash with the given parents.
func storeCommit(c *C, sto *memory.Storage, msg string, treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sig := object.Signature{Name: "Test", Email: "test@example.com", When: when}

	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      msg,
		TreeHash:     treeHash,
		ParentHashes: parents,
	}

	obj := sto.NewEncodedObject()
	c.Assert(commit.Encode(obj), IsNil)

	h, err := sto.SetEncodedObject(obj)
	c.Assert(err, IsNil)
	return h
}

// TestRevListObjects_ControlCharacterInHistory reproduces the push failure in
// issue #2251.
//
// The object walk that Push uses to decide what to send enumerates every
// reachable tree entry. It never materialises a name into a worktree, so the
// worktree-safety validation in TreeWalker.Next does not apply to it — the
// same reasoning PR #2222 established for the tree diff walk.
//
// Before the fix this fails with:
//
//	invalid path "\x1b\x1b\x1b": contains control character
//
// even though the entry exists only in history and is not part of the commit
// being pushed.
func (s *RevListSuite) TestRevListObjects_ControlCharacterInHistory(c *C) {
	sto := memory.NewStorage()

	// Distinct blobs matter. TreeWalker.Next checks its seen set *before*
	// validating the name, so an entry whose blob is still reachable from a
	// newer tree is skipped before the validator ever sees it. Content that
	// was genuinely deleted has a blob nothing else references, which is
	// what makes the historical entry reach the validator.
	deletedBlob := storeBlob(c, sto, "deleted content")
	liveBlob := storeBlob(c, sto, "live content")

	// Upstream Git's verify_path permits this name: only '/' and NUL are
	// forbidden inside a path component.
	historical := storeTree(c, sto, []object.TreeEntry{
		{Name: "\x1b\x1b\x1b", Mode: filemode.Regular, Hash: deletedBlob},
	})
	current := storeTree(c, sto, []object.TreeEntry{
		{Name: "ok.txt", Mode: filemode.Regular, Hash: liveBlob},
	})

	// The offending entry lives only in history — the commit being pushed
	// deleted it, exactly as reported.
	old := storeCommit(c, sto, "adds an oddly named file", historical)
	head := storeCommit(c, sto, "deletes it again", current, old)

	objs, err := Objects(sto, []plumbing.Hash{head}, nil)
	c.Assert(err, IsNil)

	// Both commits, both trees and both blobs must be reported as reachable.
	got := make(map[plumbing.Hash]bool, len(objs))
	for _, h := range objs {
		got[h] = true
	}
	for _, h := range []plumbing.Hash{head, old, current, historical, liveBlob, deletedBlob} {
		c.Assert(got[h], Equals, true, Commentf("missing reachable object %s", h))
	}
}

// TestRevListObjects_ControlCharacterInPushedCommit covers the same walk when
// the offending entry is in the commit being pushed rather than only in its
// history. Push still only enumerates hashes here, so this must not error.
func (s *RevListSuite) TestRevListObjects_ControlCharacterInPushedCommit(c *C) {
	sto := memory.NewStorage()

	blob := storeBlob(c, sto, "content")
	tree := storeTree(c, sto, []object.TreeEntry{
		{Name: "\x1b\x1b\x1b", Mode: filemode.Regular, Hash: blob},
	})
	head := storeCommit(c, sto, "adds an oddly named file", tree)

	objs, err := Objects(sto, []plumbing.Hash{head}, nil)
	c.Assert(err, IsNil)

	got := make(map[plumbing.Hash]bool, len(objs))
	for _, h := range objs {
		got[h] = true
	}
	for _, h := range []plumbing.Hash{head, tree, blob} {
		c.Assert(got[h], Equals, true, Commentf("missing reachable object %s", h))
	}
}
