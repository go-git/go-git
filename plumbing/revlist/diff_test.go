package revlist

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/require"
)

// assertObjectsDiffSubset verifies that ObjectsDiff returns at least every
// hash that Objects returns. ObjectsDiff may return a small number of extra
// objects (e.g. intermediate tree hashes along changed paths) which is
// acceptable—sending a few extra objects is harmless.
func assertObjectsDiffSubset(t *testing.T, s storer.EncodedObjectStorer, objs, ignore []plumbing.Hash) {
	t.Helper()

	want, err := Objects(s, objs, ignore)
	require.NoError(t, err)

	got, err := ObjectsDiff(s, objs, ignore)
	require.NoError(t, err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	for _, h := range want {
		if !gotSet[h] {
			t.Errorf("ObjectsDiff missing hash %s that Objects includes", h)
		}
	}
}

// --- Fixture-based tests (same data as RevListSuite) ---

func (s *RevListSuite) TestObjectsDiff_MatchesObjects() {
	// secondCommit relative to initialCommit
	assertObjectsDiffSubset(s.T(), s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)},
		[]plumbing.Hash{plumbing.NewHash(initialCommit)})
}

func (s *RevListSuite) TestObjectsDiff_NewBranch() {
	// Two branches relative to someCommit
	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(someCommit)}, nil)
	s.NoError(err)

	assertObjectsDiffSubset(s.T(), s.Storer,
		[]plumbing.Hash{
			plumbing.NewHash(someCommitBranch),
			plumbing.NewHash(someCommitOtherBranch),
		}, localHist)
}

func (s *RevListSuite) TestObjectsDiff_Reverse() {
	// Pushing an ancestor of what remote has → should be empty.
	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, nil)
	s.NoError(err)

	got, err := ObjectsDiff(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(initialCommit)}, localHist)
	s.NoError(err)
	s.Empty(got)
}

func (s *RevListSuite) TestObjectsDiff_SameCommit() {
	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, nil)
	s.NoError(err)

	got, err := ObjectsDiff(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, localHist)
	s.NoError(err)
	s.Empty(got)
}

func (s *RevListSuite) TestObjectsDiff_NoIgnore() {
	// No ignore = full repo push.
	assertObjectsDiffSubset(s.T(), s.Storer,
		[]plumbing.Hash{plumbing.NewHash(initialCommit)}, nil)
}

// --- Synthetic tests using memory storer ---

type memHelper struct {
	t         *testing.T
	s         *memory.Storage
	commitIdx int
}

func newMemHelper(t *testing.T) *memHelper {
	return &memHelper{t: t, s: memory.NewStorage()}
}

func (h *memHelper) blob(content string) plumbing.Hash {
	h.t.Helper()
	obj := h.s.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	require.NoError(h.t, err)
	_, err = w.Write([]byte(content))
	require.NoError(h.t, err)
	require.NoError(h.t, w.Close())
	hash, err := h.s.SetEncodedObject(obj)
	require.NoError(h.t, err)
	return hash
}

func (h *memHelper) tree(entries []object.TreeEntry) plumbing.Hash {
	h.t.Helper()
	tree := &object.Tree{Entries: entries}
	obj := h.s.NewEncodedObject()
	obj.SetType(plumbing.TreeObject)
	require.NoError(h.t, tree.Encode(obj))
	hash, err := h.s.SetEncodedObject(obj)
	require.NoError(h.t, err)
	return hash
}

func (h *memHelper) commit(treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	h.t.Helper()
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(h.commitIdx) * time.Second)
	h.commitIdx++
	c := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Message:      "test",
		TreeHash:     treeHash,
		ParentHashes: parents,
	}
	obj := h.s.NewEncodedObject()
	obj.SetType(plumbing.CommitObject)
	require.NoError(h.t, c.Encode(obj))
	hash, err := h.s.SetEncodedObject(obj)
	require.NoError(h.t, err)
	return hash
}

func TestObjectsDiff_BranchOffMain(t *testing.T) {
	t.Parallel()
	h := newMemHelper(t)

	b1 := h.blob("shared1")
	b2 := h.blob("shared2")
	b3 := h.blob("shared3")

	subTree := h.tree([]object.TreeEntry{
		{Name: "deep.txt", Mode: filemode.Regular, Hash: b3},
	})
	tree1 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b1},
		{Name: "b.txt", Mode: filemode.Regular, Hash: b2},
		{Name: "sub", Mode: filemode.Dir, Hash: subTree},
	})
	c1 := h.commit(tree1)

	b4 := h.blob("main-change")
	tree2 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b4},
		{Name: "b.txt", Mode: filemode.Regular, Hash: b2},
		{Name: "sub", Mode: filemode.Dir, Hash: subTree},
	})
	c2 := h.commit(tree2, c1)

	// Branch off c2 with one file changed.
	b5 := h.blob("branch-change")
	tree3 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b4},
		{Name: "b.txt", Mode: filemode.Regular, Hash: b5},
		{Name: "sub", Mode: filemode.Dir, Hash: subTree},
	})
	c3 := h.commit(tree3, c2)

	assertObjectsDiffSubset(t, h.s, []plumbing.Hash{c3}, []plumbing.Hash{c2})

	got, err := ObjectsDiff(h.s, []plumbing.Hash{c3}, []plumbing.Hash{c2})
	require.NoError(t, err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, gh := range got {
		gotSet[gh] = true
	}
	require.True(t, gotSet[c3], "must include new commit")
	require.True(t, gotSet[tree3], "must include new root tree")
	require.True(t, gotSet[b5], "must include changed blob")
	require.False(t, gotSet[b1], "must not include unchanged blob from c1")
	require.False(t, gotSet[b2], "must not include unchanged blob b2")
	require.False(t, gotSet[subTree], "must not include unchanged subtree")
}

func TestObjectsDiff_NewSubdirectory(t *testing.T) {
	t.Parallel()
	h := newMemHelper(t)

	b1 := h.blob("existing")
	tree1 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b1},
	})
	c1 := h.commit(tree1)

	b2 := h.blob("new-deep")
	newSub := h.tree([]object.TreeEntry{
		{Name: "new.txt", Mode: filemode.Regular, Hash: b2},
	})
	tree2 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b1},
		{Name: "dir", Mode: filemode.Dir, Hash: newSub},
	})
	c2 := h.commit(tree2, c1)

	assertObjectsDiffSubset(t, h.s, []plumbing.Hash{c2}, []plumbing.Hash{c1})

	got, err := ObjectsDiff(h.s, []plumbing.Hash{c2}, []plumbing.Hash{c1})
	require.NoError(t, err)
	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, gh := range got {
		gotSet[gh] = true
	}
	require.True(t, gotSet[newSub], "must include new subtree")
	require.True(t, gotSet[b2], "must include new blob in subtree")
	require.False(t, gotSet[b1], "must not include unchanged blob")
}

func TestObjectsDiff_DeepHistory(t *testing.T) {
	t.Parallel()
	h := newMemHelper(t)

	var prev plumbing.Hash
	for i := range 50 {
		b := h.blob(fmt.Sprintf("content-%d", i))
		tree := h.tree([]object.TreeEntry{
			{Name: "f.txt", Mode: filemode.Regular, Hash: b},
		})
		if i == 0 {
			prev = h.commit(tree)
		} else {
			prev = h.commit(tree, prev)
		}
	}
	mainTip := prev

	bNew := h.blob("branch-content")
	treeNew := h.tree([]object.TreeEntry{
		{Name: "f.txt", Mode: filemode.Regular, Hash: bNew},
	})
	branchTip := h.commit(treeNew, mainTip)

	assertObjectsDiffSubset(t, h.s, []plumbing.Hash{branchTip}, []plumbing.Hash{mainTip})

	got, err := ObjectsDiff(h.s, []plumbing.Hash{branchTip}, []plumbing.Hash{mainTip})
	require.NoError(t, err)
	require.Len(t, got, 3, "should only send 1 new commit + tree + blob")
}

func TestObjectsDiff_RootCommit(t *testing.T) {
	t.Parallel()
	h := newMemHelper(t)

	b1 := h.blob("file1")
	b2 := h.blob("file2")
	tree := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b1},
		{Name: "b.txt", Mode: filemode.Regular, Hash: b2},
	})
	c1 := h.commit(tree)

	assertObjectsDiffSubset(t, h.s, []plumbing.Hash{c1}, nil)
}

func TestObjectsDiff_MultipleNewCommits(t *testing.T) {
	t.Parallel()
	h := newMemHelper(t)

	b1 := h.blob("original")
	tree1 := h.tree([]object.TreeEntry{
		{Name: "f.txt", Mode: filemode.Regular, Hash: b1},
	})
	c1 := h.commit(tree1)

	b2 := h.blob("change1")
	tree2 := h.tree([]object.TreeEntry{
		{Name: "f.txt", Mode: filemode.Regular, Hash: b2},
	})
	c2 := h.commit(tree2, c1)

	b3 := h.blob("change2")
	tree3 := h.tree([]object.TreeEntry{
		{Name: "f.txt", Mode: filemode.Regular, Hash: b3},
	})
	c3 := h.commit(tree3, c2)

	assertObjectsDiffSubset(t, h.s, []plumbing.Hash{c3}, []plumbing.Hash{c1})
}

func TestObjectsDiff_MultipleTipsBothSides(t *testing.T) {
	t.Parallel()
	h := newMemHelper(t)

	// Two independent histories with separate remote branches.
	//
	// History 1:  r1 --- r2  (remote: main)
	//                \
	//                 l1     (local: feature-1)
	//
	// History 2:  r3 --- r4  (remote: releases/v1)
	//                \
	//                 l2     (local: feature-2)

	// History 1
	b1 := h.blob("shared-1")
	tree1 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b1},
	})
	r1 := h.commit(tree1)

	b2 := h.blob("main-update")
	tree2 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b2},
	})
	r2 := h.commit(tree2, r1)

	b3 := h.blob("feature-1-change")
	tree3 := h.tree([]object.TreeEntry{
		{Name: "a.txt", Mode: filemode.Regular, Hash: b3},
	})
	l1 := h.commit(tree3, r1)

	// History 2
	b4 := h.blob("shared-2")
	tree4 := h.tree([]object.TreeEntry{
		{Name: "b.txt", Mode: filemode.Regular, Hash: b4},
	})
	r3 := h.commit(tree4)

	b5 := h.blob("release-update")
	tree5 := h.tree([]object.TreeEntry{
		{Name: "b.txt", Mode: filemode.Regular, Hash: b5},
	})
	r4 := h.commit(tree5, r3)

	b6 := h.blob("feature-2-change")
	tree6 := h.tree([]object.TreeEntry{
		{Name: "b.txt", Mode: filemode.Regular, Hash: b6},
	})
	l2 := h.commit(tree6, r3)

	localTips := []plumbing.Hash{l1, l2}
	remoteTips := []plumbing.Hash{r2, r4}

	assertObjectsDiffSubset(t, h.s, localTips, remoteTips)

	got, err := ObjectsDiff(h.s, localTips, remoteTips)
	require.NoError(t, err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, gh := range got {
		gotSet[gh] = true
	}

	// Must include new commits and their objects.
	require.True(t, gotSet[l1], "must include feature-1 commit")
	require.True(t, gotSet[l2], "must include feature-2 commit")
	require.True(t, gotSet[b3], "must include feature-1 blob")
	require.True(t, gotSet[b6], "must include feature-2 blob")

	// Must not include remote-only objects.
	require.False(t, gotSet[r2], "must not include remote main tip")
	require.False(t, gotSet[r4], "must not include remote release tip")
	require.False(t, gotSet[b2], "must not include remote main blob")
	require.False(t, gotSet[b5], "must not include remote release blob")

	// Must not include shared ancestors.
	require.False(t, gotSet[r1], "must not include shared ancestor r1")
	require.False(t, gotSet[r3], "must not include shared ancestor r3")
}

// --- Benchmarks ---

func buildBenchRepo(b *testing.B, numCommits, numFiles int) (*memory.Storage, plumbing.Hash, plumbing.Hash) {
	b.Helper()
	s := memory.NewStorage()
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	commitIdx := 0

	mkBlob := func(content string) plumbing.Hash {
		obj := s.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		w, _ := obj.Writer()
		w.Write([]byte(content))
		w.Close()
		h, _ := s.SetEncodedObject(obj)
		return h
	}
	mkTree := func(entries []object.TreeEntry) plumbing.Hash {
		tree := &object.Tree{Entries: entries}
		obj := s.NewEncodedObject()
		obj.SetType(plumbing.TreeObject)
		tree.Encode(obj)
		h, _ := s.SetEncodedObject(obj)
		return h
	}
	mkCommit := func(treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
		when := baseTime.Add(time.Duration(commitIdx) * time.Second)
		commitIdx++
		c := &object.Commit{
			Author:       object.Signature{Name: "B", Email: "b@b.com", When: when},
			Committer:    object.Signature{Name: "B", Email: "b@b.com", When: when},
			Message:      "b",
			TreeHash:     treeHash,
			ParentHashes: parents,
		}
		obj := s.NewEncodedObject()
		obj.SetType(plumbing.CommitObject)
		c.Encode(obj)
		h, _ := s.SetEncodedObject(obj)
		return h
	}

	sharedBlobs := make([]plumbing.Hash, numFiles)
	for i := range numFiles {
		sharedBlobs[i] = mkBlob(fmt.Sprintf("shared-%d", i))
	}

	var prev plumbing.Hash
	for i := range numCommits {
		entries := make([]object.TreeEntry, numFiles)
		for j := range numFiles {
			entries[j] = object.TreeEntry{
				Name: fmt.Sprintf("file%03d.txt", j),
				Mode: filemode.Regular,
				Hash: sharedBlobs[j],
			}
		}
		changed := mkBlob(fmt.Sprintf("changed-%d", i))
		entries[0] = object.TreeEntry{Name: "file000.txt", Mode: filemode.Regular, Hash: changed}
		tree := mkTree(entries)
		if i == 0 {
			prev = mkCommit(tree)
		} else {
			prev = mkCommit(tree, prev)
		}
	}
	mainTip := prev

	entries := make([]object.TreeEntry, numFiles)
	for j := range numFiles {
		entries[j] = object.TreeEntry{
			Name: fmt.Sprintf("file%03d.txt", j),
			Mode: filemode.Regular,
			Hash: sharedBlobs[j],
		}
	}
	entries[1] = object.TreeEntry{
		Name: "file001.txt",
		Mode: filemode.Regular,
		Hash: mkBlob("branch-change"),
	}
	branchTree := mkTree(entries)
	branchTip := mkCommit(branchTree, mainTip)

	return s, mainTip, branchTip
}

func BenchmarkObjects(b *testing.B) {
	s, mainTip, branchTip := buildBenchRepo(b, 200, 100)
	b.ResetTimer()
	for range b.N {
		_, err := Objects(s, []plumbing.Hash{branchTip}, []plumbing.Hash{mainTip})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkObjectsDiff(b *testing.B) {
	s, mainTip, branchTip := buildBenchRepo(b, 200, 100)
	b.ResetTimer()
	for range b.N {
		_, err := ObjectsDiff(s, []plumbing.Hash{branchTip}, []plumbing.Hash{mainTip})
		if err != nil {
			b.Fatal(err)
		}
	}
}
