package revlist_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestStreamParityWithObjects builds a small in-memory repo, runs both
// Stream and Objects on the same (wants, haves) input, and asserts they
// produce the same hash set.
func TestStreamParityWithObjects(t *testing.T) {
	t.Parallel()

	storer := memory.NewStorage()
	wants, haves := buildSmallFixture(t, storer)

	expected, err := revlist.Objects(storer, wants, haves)
	if err != nil {
		t.Fatalf("Objects: %v", err)
	}

	out := make(chan revlist.Entry, 16)
	var streamed []plumbing.Hash
	streamErr := make(chan error, 1)
	go func() {
		_, err := revlist.Stream(context.Background(), storer, wants, haves, out)
		streamErr <- err
	}()
	for e := range out {
		streamed = append(streamed, e.Hash)
	}
	if err := <-streamErr; err != nil {
		t.Fatalf("Stream: %v", err)
	}

	sortHashes(expected)
	sortHashes(streamed)
	if !equalHashes(expected, streamed) {
		t.Fatalf("Stream and Objects disagree:\n want: %v\n got:  %v", expected, streamed)
	}
}

// TestStreamYieldsCorrectTypes verifies that every Entry emitted by Stream
// has a Type that matches the actual stored object type. This catches bugs
// where the wrong type is emitted while the hash is correct.
func TestStreamYieldsCorrectTypes(t *testing.T) {
	t.Parallel()

	storer := memory.NewStorage()
	wants, haves := buildSmallFixture(t, storer)

	out := make(chan revlist.Entry, 16)
	streamErr := make(chan error, 1)
	go func() {
		_, err := revlist.Stream(context.Background(), storer, wants, haves, out)
		streamErr <- err
	}()

	for e := range out {
		obj, err := storer.EncodedObject(plumbing.AnyObject, e.Hash)
		if err != nil {
			t.Fatalf("failed to retrieve object %s: %v", e.Hash, err)
		}
		actualType := obj.Type()
		if e.Type != actualType {
			t.Fatalf("Entry %s has Type %v but stored object has Type %v",
				e.Hash, e.Type, actualType)
		}
	}

	if err := <-streamErr; err != nil {
		t.Fatalf("Stream: %v", err)
	}
}

// buildSmallFixture creates a tiny linear repo:
//
//	C1 (root: blob1, blob2) ← C2 (adds blob3) ← C3 (modifies blob1)
//
// Returns wants=[C3] haves=[C1] so the walk exercises commits, trees, and blobs.
func buildSmallFixture(t *testing.T, s *memory.Storage) (wants, haves []plumbing.Hash) {
	t.Helper()

	makeBlob := func(content string) plumbing.Hash {
		obj := s.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		w, err := obj.Writer()
		if err != nil {
			t.Fatalf("blob writer: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("blob write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("blob close: %v", err)
		}
		h, err := s.SetEncodedObject(obj)
		if err != nil {
			t.Fatalf("set blob: %v", err)
		}
		return h
	}

	makeTree := func(entries []object.TreeEntry) plumbing.Hash {
		tree := &object.Tree{Entries: entries}
		obj := s.NewEncodedObject()
		obj.SetType(plumbing.TreeObject)
		if err := tree.Encode(obj); err != nil {
			t.Fatalf("tree encode: %v", err)
		}
		h, err := s.SetEncodedObject(obj)
		if err != nil {
			t.Fatalf("set tree: %v", err)
		}
		return h
	}

	var seq int
	makeCommit := func(treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
		seq++
		when := time.Date(2024, 1, 1, 0, 0, seq, 0, time.UTC)
		c := &object.Commit{
			Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when},
			Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when},
			Message:      "test",
			TreeHash:     treeHash,
			ParentHashes: parents,
		}
		obj := s.NewEncodedObject()
		obj.SetType(plumbing.CommitObject)
		if err := c.Encode(obj); err != nil {
			t.Fatalf("commit encode: %v", err)
		}
		h, err := s.SetEncodedObject(obj)
		if err != nil {
			t.Fatalf("set commit: %v", err)
		}
		return h
	}

	blob1 := makeBlob("content of file1\n")
	blob2 := makeBlob("content of file2\n")
	blob3 := makeBlob("content of file3\n")
	blob1v2 := makeBlob("modified content of file1\n")

	tree1 := makeTree([]object.TreeEntry{
		{Name: "file1", Mode: filemode.Regular, Hash: blob1},
		{Name: "file2", Mode: filemode.Regular, Hash: blob2},
	})
	c1 := makeCommit(tree1)

	tree2 := makeTree([]object.TreeEntry{
		{Name: "file1", Mode: filemode.Regular, Hash: blob1},
		{Name: "file2", Mode: filemode.Regular, Hash: blob2},
		{Name: "file3", Mode: filemode.Regular, Hash: blob3},
	})
	c2 := makeCommit(tree2, c1)

	tree3 := makeTree([]object.TreeEntry{
		{Name: "file1", Mode: filemode.Regular, Hash: blob1v2},
		{Name: "file2", Mode: filemode.Regular, Hash: blob2},
		{Name: "file3", Mode: filemode.Regular, Hash: blob3},
	})
	c3 := makeCommit(tree3, c2)

	return []plumbing.Hash{c3}, []plumbing.Hash{c1}
}

func sortHashes(h []plumbing.Hash) {
	sort.Slice(h, func(i, j int) bool { return h[i].String() < h[j].String() })
}

func equalHashes(a, b []plumbing.Hash) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestStreamContextCancelled verifies that canceling the context unblocks
// a stalled walker. This is critical for T6's loader pool, which will cancel
// contexts to interrupt in-flight walks.
func TestStreamContextCancelled(t *testing.T) {
	t.Parallel()
	s := memory.NewStorage()
	wants, _ := buildSmallFixture(t, s)

	ctx, cancel := context.WithCancel(context.Background())

	out := make(chan revlist.Entry) // unbuffered so writer blocks on first emit
	streamErr := make(chan error, 1)
	go func() {
		_, err := revlist.Stream(ctx, s, wants, nil, out)
		streamErr <- err
	}()

	// Read one entry to make sure the walker has started, then cancel.
	select {
	case _, ok := <-out:
		if !ok {
			t.Fatal("channel closed before any entry")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream produced no entries within 2s")
	}
	cancel()

	// Drain remaining entries; the walker may have produced a few more
	// before observing the cancel.
	for range out {
	}

	select {
	case err := <-streamErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return after context cancel")
	}
}
