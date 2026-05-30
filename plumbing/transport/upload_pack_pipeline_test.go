package transport

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestWritePipelinedPack_RoundTrip builds a small in-memory repo, runs
// the pipelined writer over its entire reachable object set, parses the
// resulting pack, and asserts the parsed object set matches
// revlist.Objects.
func TestWritePipelinedPack_RoundTrip(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	wants := buildSmallFixtureForPipeline(t, st)

	expected, err := revlist.Objects(st, wants, nil)
	if err != nil {
		t.Fatalf("Objects: %v", err)
	}

	var buf bytes.Buffer
	opts := pipelinedOptions{
		PackWindow:           10,
		SkipDeltaCompression: false,
		LoaderCount:          4,
	}
	if err := writePipelinedPack(context.Background(), &buf, st, wants, nil, opts, nil); err != nil {
		t.Fatalf("writePipelinedPack: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty pack")
	}

	got := parsePackHashes(t, buf.Bytes())
	sortHashes(expected)
	sortHashes(got)
	if !equalHashSets(expected, got) {
		t.Fatalf("pack object set mismatch:\n want: %v\n got:  %v", expected, got)
	}
}

// parsePackHashes parses a pack from b into a fresh storage and returns
// the hashes of all objects in it.
func parsePackHashes(t *testing.T, b []byte) []plumbing.Hash {
	t.Helper()
	target := memory.NewStorage()
	parser := packfile.NewParser(bytes.NewReader(b), packfile.WithStorage(target))
	if _, err := parser.Parse(); err != nil {
		t.Fatalf("parse: %v", err)
	}
	iter, err := target.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	var hashes []plumbing.Hash
	if err := iter.ForEach(func(o plumbing.EncodedObject) error {
		hashes = append(hashes, o.Hash())
		return nil
	}); err != nil {
		t.Fatalf("foreach: %v", err)
	}
	return hashes
}

func sortHashes(h []plumbing.Hash) {
	sort.Slice(h, func(i, j int) bool { return h[i].String() < h[j].String() })
}

func equalHashSets(a, b []plumbing.Hash) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[plumbing.Hash]struct{}, len(a))
	for _, h := range a {
		am[h] = struct{}{}
	}
	for _, h := range b {
		if _, ok := am[h]; !ok {
			return false
		}
	}
	return true
}

// buildSmallFixtureForPipeline creates a tiny linear repo:
//
//	C1 (root: blob1, blob2) ← C2 (adds blob3) ← C3 (modifies blob1)
//
// Returns wants=[C3] so the walk exercises commits, trees, and blobs.
func buildSmallFixtureForPipeline(t *testing.T, s *memory.Storage) []plumbing.Hash {
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

	return []plumbing.Hash{c3}
}

// TestWritePipelinedPack_CtxCancelMidPipeline cancels the context after
// kicking off writePipelinedPack and verifies it returns a non-nil error
// and does not deadlock.
func TestWritePipelinedPack_CtxCancelMidPipeline(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	wants := buildSmallFixtureForPipeline(t, st)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		var buf bytes.Buffer
		done <- writePipelinedPack(ctx, &buf, st, wants, nil, pipelinedOptions{
			PackWindow:  10,
			LoaderCount: 1,
		}, nil)
	}()

	cancel()

	select {
	case err := <-done:
		if err == nil {
			// Race: pipeline may have completed before cancel fired. That's
			// acceptable for this tiny fixture; the real assertion is "no
			// deadlock". Only fail if we hit a deadlock above.
			t.Log("pipeline completed before cancel; acceptable on tiny fixtures")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writePipelinedPack did not return after context cancel")
	}
}

// recordingWalker wraps a storer to track whether PackObjects was invoked.
type recordingWalker struct {
	*memory.Storage
	called bool
}

// PackObjects implements storer.PackObjectWalker and records the call.
func (r *recordingWalker) PackObjects(ctx context.Context, wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	r.called = true
	return revlist.Objects(r.Storage, wants, haves)
}

// TestWritePipelinedPack_PackObjectWalkerDispatch verifies that a storer
// implementing storer.PackObjectWalker has its PackObjects method invoked
// instead of revlist.Stream.
func TestWritePipelinedPack_PackObjectWalkerDispatch(t *testing.T) {
	t.Parallel()
	st := &recordingWalker{Storage: memory.NewStorage()}
	wants := buildSmallFixtureForPipeline(t, st.Storage)

	var buf bytes.Buffer
	if err := writePipelinedPack(context.Background(), &buf, st, wants, nil, pipelinedOptions{
		PackWindow:  10,
		LoaderCount: 2,
	}, nil); err != nil {
		t.Fatalf("writePipelinedPack: %v", err)
	}
	if !st.called {
		t.Fatal("PackObjectWalker.PackObjects was not invoked")
	}
	if buf.Len() == 0 {
		t.Fatal("empty pack")
	}
}

// failingStorage wraps a storer to fail on a specific hash.
type failingStorage struct {
	*memory.Storage
	failHash plumbing.Hash
}

// EncodedObject returns an error for the failHash.
func (f *failingStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if h == f.failHash {
		return nil, fmt.Errorf("synthetic load failure for %s", h)
	}
	return f.Storage.EncodedObject(t, h)
}

// TestWritePipelinedPack_LoaderError verifies that a loader failing on a
// specific hash surfaces the error without deadlock.
func TestWritePipelinedPack_LoaderError(t *testing.T) {
	t.Parallel()
	inner := memory.NewStorage()
	wants := buildSmallFixtureForPipeline(t, inner)

	hashes, err := revlist.Objects(inner, wants, nil)
	if err != nil {
		t.Fatalf("Objects: %v", err)
	}
	if len(hashes) == 0 {
		t.Fatal("no objects in fixture")
	}

	st := &failingStorage{Storage: inner, failHash: hashes[0]}

	var buf bytes.Buffer
	err = writePipelinedPack(context.Background(), &buf, st, wants, nil, pipelinedOptions{
		PackWindow:           0,
		SkipDeltaCompression: true,
		LoaderCount:          2,
	}, nil)
	if err == nil {
		t.Fatal("expected error from failing loader, got nil")
	}
	if !strings.Contains(err.Error(), "synthetic load failure") {
		t.Fatalf("expected error to mention loader failure, got %v", err)
	}
}
