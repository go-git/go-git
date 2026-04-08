package revlist

import (
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type RevListFixtureSuite struct{}

type RevListSuite struct {
	suite.Suite
	RevListFixtureSuite
	Storer storer.EncodedObjectStorer
}

func TestRevListSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(RevListSuite))
}

const (
	initialCommit = "b029517f6300c2da0f4b651b8642506cd6aaf45d"
	secondCommit  = "b8e471f58bcbca63b07bda20e428190409c2db47"

	someCommit            = "918c48b83bd081e863dbe1b80f8998f058cd8294"
	someCommitBranch      = "e8d3ffab552895c19b9fcf7aa264d277cde33881"
	someCommitOtherBranch = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
)

// Created using: git log --graph --oneline --all
//
// Basic fixture repository commits tree:
//
// * 6ecf0ef vendor stuff
// | * e8d3ffa some code in a branch
// |/
// * 918c48b some code
// * af2d6a6 some json
// *   1669dce Merge branch 'master'
// |\
// | *   a5b8b09 Merge pull request #1
// | |\
// | | * b8e471f Creating changelog
// | |/
// * | 35e8510 binary file
// |/
// * b029517 Initial commit

func (s *RevListSuite) SetupTest() {
	dotgit, err := fixtures.Basic().One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	s.Storer = sto
}

func (s *RevListSuite) TestRevListObjects_Submodules() {
	submodules := map[string]bool{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5": true,
	}

	subDotgit, err := fixtures.ByTag("submodule").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(subDotgit, cache.NewObjectLRUDefault())

	ref, err := storer.ResolveReference(sto, plumbing.HEAD)
	s.NoError(err)

	revList, err := Objects(sto, []plumbing.Hash{ref.Hash()}, nil)
	s.NoError(err)
	for _, h := range revList {
		s.False(submodules[h.String()])
	}
}

// ---
// | |\
// | | * b8e471f Creating changelog
// | |/
// * | 35e8510 binary file
// |/
// * b029517 Initial commit
func (s *RevListSuite) TestRevListObjects() {
	revList := map[string]bool{
		"b8e471f58bcbca63b07bda20e428190409c2db47": true, // second commit
		"c2d30fa8ef288618f65f6eed6e168e0d514886f4": true, // init tree
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa": true, // CHANGELOG
	}

	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(initialCommit)}, nil)
	s.NoError(err)

	remoteHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, localHist)
	s.NoError(err)

	for _, h := range remoteHist {
		s.True(revList[h.String()])
	}
	s.Len(revList, len(remoteHist))
}

func (s *RevListSuite) TestRevListObjectsTagObject() {
	sto := filesystem.NewStorage(

		func() billy.Filesystem {
			d, err := fixtures.ByTag("tags").ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
			s.Require().NoError(err)
			return d
		}(), cache.NewObjectLRUDefault())

	expected := map[string]bool{
		"70846e9a10ef7b41064b40f07713d5b8b9a8fc73": true,
		"e69de29bb2d1d6434b8b29ae775ad8c2e48c5391": true,
		"ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc": true,
		"f7b877701fbf855b44c0a9e86f3fdce2c298b07f": true,
	}

	hist, err := Objects(sto, []plumbing.Hash{plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc")}, nil)
	s.NoError(err)

	for _, h := range hist {
		s.True(expected[h.String()])
	}

	s.Len(expected, len(hist))
}

func (s *RevListSuite) TestRevListObjectsReverse() {
	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, nil)
	s.NoError(err)

	remoteHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(initialCommit)}, localHist)
	s.NoError(err)

	s.Len(remoteHist, 0)
}

func (s *RevListSuite) TestRevListObjectsSameCommit() {
	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, nil)
	s.NoError(err)

	remoteHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(secondCommit)}, localHist)
	s.NoError(err)

	s.Len(remoteHist, 0)
}

// * 6ecf0ef vendor stuff
// | * e8d3ffa some code in a branch
// |/
// * 918c48b some code
// -----
func (s *RevListSuite) TestRevListObjectsNewBranch() {
	localHist, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(someCommit)}, nil)
	s.NoError(err)

	remoteHist, err := Objects(
		s.Storer, []plumbing.Hash{
			plumbing.NewHash(someCommitBranch),
			plumbing.NewHash(someCommitOtherBranch),
		}, localHist)
	s.NoError(err)

	revList := map[string]bool{
		"a8d315b2b1c615d43042c3a62402b8a54288cf5c": true, // init tree
		"cf4aa3b38974fb7d81f367c0830f7d78d65ab86b": true, // vendor folder
		"9dea2395f5403188298c1dabe8bdafe562c491e3": true, // foo.go
		"e8d3ffab552895c19b9fcf7aa264d277cde33881": true, // branch commit
		"dbd3641b371024f44d0e469a9c8f5457b0660de1": true, // init tree
		"7e59600739c96546163833214c36459e324bad0a": true, // README
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5": true, // otherBranch commit
	}

	for _, h := range remoteHist {
		s.True(revList[h.String()])
	}
	s.Len(revList, len(remoteHist))
}

// This test ensures that commits reachable through merge branches
// (a5b8b09, b8e471f) are included even when another branch (35e8510)
// is in haves.
//
// * af2d6a6 some json
// *   1669dce Merge branch 'master'
// |\
// | *   a5b8b09 Merge pull request #1
// | |\
// | | * b8e471f Creating changelog
// | |/
// * | 35e8510 binary file
// |/
// * b029517 Initial commit
func (s *RevListSuite) TestReachableObjectsNoRevisit() {
	got, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")},
		[]plumbing.Hash{plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")})
	s.NoError(err)

	// Filter to just commits and verify merge-branch commits (a5b8b09,
	// b8e471f) are included despite 35e8510 being in haves.
	gotSet := make(map[plumbing.Hash]bool)
	for _, h := range got {
		if _, err := object.GetCommit(s.Storer, h); err == nil {
			gotSet[h] = true
		}
	}
	expected := []plumbing.Hash{
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	}
	for _, h := range expected {
		s.True(gotSet[h])
	}
	s.Len(gotSet, len(expected))
}

func (s *RevListSuite) TestRevListObjectsNoIgnore() {
	// No ignore = full repo push of initial commit.
	expected := map[string]bool{
		"b029517f6300c2da0f4b651b8642506cd6aaf45d": true, // initial commit
		"aa9b383c260e1d05fbbf6b30a02914555e20c725": true, // root tree
		"32858aad3c383ed1ff0a0f9bdf231d54a00c9e88": true, // LICENSE
		"c192bd6a24ea1ab01d78686e417c8bdc7c3d197f": true, // .gitignore
	}

	got, err := Objects(s.Storer,
		[]plumbing.Hash{plumbing.NewHash(initialCommit)}, nil)
	s.NoError(err)

	for _, h := range got {
		s.True(expected[h.String()])
	}
	s.Len(expected, len(got))
}

func (s *RevListSuite) TestRevListObjectsBlobWant() {
	// Pushing a bare blob by OID should return just that blob.
	blobHash := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88") // LICENSE
	got, err := Objects(s.Storer, []plumbing.Hash{blobHash}, nil)
	s.NoError(err)
	s.Equal([]plumbing.Hash{blobHash}, got)
}

func (s *RevListSuite) TestRevListObjectsTreeWant() {
	// Pushing a bare tree by OID should return the tree and all its contents.
	treeHash := plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725") // root tree of initial commit
	got, err := Objects(s.Storer, []plumbing.Hash{treeHash}, nil)
	s.NoError(err)

	gotSet := make(map[string]bool, len(got))
	for _, h := range got {
		gotSet[h.String()] = true
	}
	s.True(gotSet["aa9b383c260e1d05fbbf6b30a02914555e20c725"]) // root tree
	s.True(gotSet["32858aad3c383ed1ff0a0f9bdf231d54a00c9e88"]) // LICENSE
	s.True(gotSet["c192bd6a24ea1ab01d78686e417c8bdc7c3d197f"]) // .gitignore
	s.Len(got, 3)
}

// --- Regression tests ---
//
// These build new objects on top of fixtures.Basic() to trigger
// specific edge cases around tree hash reuse and haves exclusion.
// Known objects from the basic fixture's initial commit (b029517):
//   root tree: aa9b383c260e1d05fbbf6b30a02914555e20c725
//   LICENSE:   32858aad3c383ed1ff0a0f9bdf231d54a00c9e88
//   .gitignore: c192bd6a24ea1ab01d78686e417c8bdc7c3d197f

func (s *RevListSuite) makeTree(entries []object.TreeEntry) plumbing.Hash {
	s.T().Helper()
	tree := &object.Tree{Entries: entries}
	obj := s.Storer.NewEncodedObject()
	obj.SetType(plumbing.TreeObject)
	s.Require().NoError(tree.Encode(obj))
	hash, err := s.Storer.SetEncodedObject(obj)
	s.Require().NoError(err)
	return hash
}

func (s *RevListSuite) makeBlob(contents string) plumbing.Hash {
	s.T().Helper()
	obj := s.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	s.Require().NoError(err)
	_, err = w.Write([]byte(contents))
	s.Require().NoError(err)
	s.Require().NoError(w.Close())
	hash, err := s.Storer.SetEncodedObject(obj)
	s.Require().NoError(err)
	return hash
}

var commitCounter int

func (s *RevListSuite) makeCommit(treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	s.T().Helper()
	commitCounter++
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(commitCounter) * time.Second)
	return s.makeCommitAt(treeHash, when, parents...)
}

func (s *RevListSuite) makeCommitAt(treeHash plumbing.Hash, when time.Time, parents ...plumbing.Hash) plumbing.Hash {
	s.T().Helper()
	c := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Message:      "test",
		TreeHash:     treeHash,
		ParentHashes: parents,
	}
	obj := s.Storer.NewEncodedObject()
	obj.SetType(plumbing.CommitObject)
	s.Require().NoError(c.Encode(obj))
	hash, err := s.Storer.SetEncodedObject(obj)
	s.Require().NoError(err)
	return hash
}

func (s *RevListSuite) TestRevListObjects_RevertedSubtreeMatchesRoot() {
	// The initial commit's root tree (aa9b383c) is used as subtree S.
	// P modifies one entry, then C reverts S back. All blobs in S must
	// be collected even when S is already in the seen set.
	//
	// Root: {dir/ → S(=aa9b383c) = {LICENSE, .gitignore}}
	// P:    {dir/ → S_mod = {LICENSE_new, .gitignore}}
	// C:    {dir/ → S(=aa9b383c), extra}
	license := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	gitignore := plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")
	initialTree := plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725") // = S

	treeRoot := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: initialTree},
	})
	cRoot := s.makeCommit(treeRoot)

	// P: modify LICENSE inside dir/.
	subMod := s.makeTree([]object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: gitignore},
		{Name: "LICENSE", Mode: filemode.Regular, Hash: gitignore}, // different content
	})
	treeP := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: subMod},
	})
	cP := s.makeCommit(treeP, cRoot)

	// C: revert dir/ back to S, add an extra file.
	treeC := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: initialTree},
		{Name: "extra", Mode: filemode.Regular, Hash: license},
	})
	cC := s.makeCommit(treeC, cP)

	got, err := Objects(s.Storer, []plumbing.Hash{cC}, nil)
	s.NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, gh := range got {
		gotSet[gh] = true
	}

	s.True(gotSet[license], "must include LICENSE blob")
	s.True(gotSet[gitignore], "must include .gitignore blob")
}

func (s *RevListSuite) TestRevListObjects_SeenSubtreeDifferentParent() {
	// The initial commit's root tree (aa9b383c) is used as subtree S.
	// S appears in two commits with different parent trees, so different
	// entries are "new" in each diff. All entries must be collected.
	//
	// Base:  {dir/ → S_base = {LICENSE_old, .gitignore}}
	// Older: {dir/ → S(=aa9b383c)}                       (parent Base)
	// P_new: {dir/ → S_mod = {LICENSE, .gitignore_new}}
	// Newer: {dir/ → S(=aa9b383c), extra}                (parent P_new)
	license := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	gitignore := plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")
	initialTree := plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725") // = S

	// Base: dir/ has .gitignore but a different LICENSE.
	subBase := s.makeTree([]object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: gitignore},
		{Name: "LICENSE", Mode: filemode.Regular, Hash: gitignore}, // wrong content
	})
	treeBase := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: subBase},
	})
	cBase := s.makeCommit(treeBase)

	// Older: fixes LICENSE → dir/ becomes S.
	treeOlder := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: initialTree},
	})
	cOlder := s.makeCommit(treeOlder, cBase)

	// P_new: has LICENSE but a different .gitignore.
	subPNew := s.makeTree([]object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: license}, // wrong content
		{Name: "LICENSE", Mode: filemode.Regular, Hash: license},
	})
	treePNew := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: subPNew},
	})
	cPNew := s.makeCommit(treePNew, cOlder)

	// Newer: reverts dir/ to S, adds extra.
	treeNewer := s.makeTree([]object.TreeEntry{
		{Name: "dir", Mode: filemode.Dir, Hash: initialTree},
		{Name: "extra", Mode: filemode.Regular, Hash: license},
	})
	cNewer := s.makeCommit(treeNewer, cPNew)

	got, err := Objects(s.Storer, []plumbing.Hash{cNewer}, nil)
	s.NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, gh := range got {
		gotSet[gh] = true
	}

	s.True(gotSet[license], "must include LICENSE blob")
	s.True(gotSet[gitignore], "must include .gitignore blob")
}

func (s *RevListSuite) TestRevListObjects_ReintroducedBlobInHaves() {
	// Uses the initial commit (b029517) as haves. Its tree has LICENSE
	// and .gitignore. A new commit removes LICENSE, then another re-adds
	// it (reusing the same tree hash). Neither blob should appear in the
	// result since haves already has them.
	haveCommit := plumbing.NewHash(initialCommit)
	license := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	gitignore := plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")
	initialTree := plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725")

	// Remove LICENSE.
	treeRemove := s.makeTree([]object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: gitignore},
	})
	cRemove := s.makeCommit(treeRemove, haveCommit)

	// Re-add LICENSE — same tree hash as initial commit.
	cReAdd := s.makeCommit(initialTree, cRemove)

	got, err := Objects(s.Storer, []plumbing.Hash{cReAdd}, []plumbing.Hash{haveCommit})
	s.NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, gh := range got {
		gotSet[gh] = true
	}

	s.False(gotSet[license], "LICENSE is in haves, must not be included")
	s.False(gotSet[gitignore], ".gitignore is in haves, must not be included")
}

func (s *RevListSuite) TestRevListObjects_ReintroducedBlobInHaveAncestorIsExcluded() {
	oldBlob := s.makeBlob("old")
	newBlob := s.makeBlob("new")

	baseTree := s.makeTree([]object.TreeEntry{
		{Name: "x", Mode: filemode.Regular, Hash: oldBlob},
	})
	base := s.makeCommitAt(baseTree, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	haveTree := s.makeTree(nil)
	have := s.makeCommitAt(haveTree, time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC), base)

	wantTree := s.makeTree([]object.TreeEntry{
		{Name: "x", Mode: filemode.Regular, Hash: oldBlob},
		{Name: "y", Mode: filemode.Regular, Hash: newBlob},
	})
	want := s.makeCommitAt(wantTree, time.Date(2024, 1, 1, 0, 2, 0, 0, time.UTC), have)

	got, err := Objects(s.Storer, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.Require().NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	s.False(gotSet[oldBlob], "objects reachable from haves ancestry must not be included")
	s.True(gotSet[newBlob], "new objects reachable only from wants must be included")
}

func (s *RevListSuite) TestRevListObjects_MissingHaveAncestorReturnsError() {
	haveTree := s.makeTree([]object.TreeEntry{
		{Name: "have", Mode: filemode.Regular, Hash: s.makeBlob("have")},
	})
	missingAncestor := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	have := s.makeCommitAt(haveTree, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), missingAncestor)

	wantTree := s.makeTree([]object.TreeEntry{
		{Name: "want", Mode: filemode.Regular, Hash: s.makeBlob("want")},
	})
	want := s.makeCommitAt(wantTree, time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC), have)

	_, err := Objects(s.Storer, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.Error(err, "missing non-shallow have ancestors must not be ignored")
}

func (s *RevListSuite) TestRevListObjects_MissingWantParentTreeReturnsError() {
	missingTree := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	parent := s.makeCommitAt(missingTree, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	wantTree := s.makeTree([]object.TreeEntry{
		{Name: "want", Mode: filemode.Regular, Hash: s.makeBlob("want")},
	})
	want := s.makeCommitAt(wantTree, time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC), parent)

	_, err := Objects(s.Storer, []plumbing.Hash{want}, []plumbing.Hash{parent})
	s.Error(err, "missing trees for reachable want parents must not be ignored")
}

func (s *RevListSuite) TestRevListObjects_SkewedCommitTimesDoNotResendCommonBase() {
	// Reachability is graph-based, not timestamp-based. Even when the shared
	// base commit has a later committer time than both child tips, it is
	// already reachable from haves and must not be emitted.
	license := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	gitignore := plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")

	baseTree := s.makeTree([]object.TreeEntry{
		{Name: "shared", Mode: filemode.Regular, Hash: license},
	})
	base := s.makeCommitAt(baseTree, time.Date(2024, 1, 1, 0, 2, 0, 0, time.UTC))

	haveTree := s.makeTree([]object.TreeEntry{
		{Name: "shared", Mode: filemode.Regular, Hash: license},
	})
	have := s.makeCommitAt(haveTree, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), base)

	wantTree := s.makeTree([]object.TreeEntry{
		{Name: "unique", Mode: filemode.Regular, Hash: gitignore},
	})
	want := s.makeCommitAt(wantTree, time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC), base)

	got, err := Objects(s.Storer, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.Require().NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	s.True(gotSet[want], "want tip must be included")
	s.True(gotSet[wantTree], "want tree must be included")
	s.True(gotSet[gitignore], "new blob must be included")
	s.False(gotSet[base], "common base reachable from haves must not be included")
}

func (s *RevListSuite) TestRevListObjects_ShallowTaggedCommitStopsAtBoundary() {
	sto := memory.NewStorage()

	blobObj := sto.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	blobWriter, err := blobObj.Writer()
	s.Require().NoError(err)
	_, err = blobWriter.Write([]byte("README\n"))
	s.Require().NoError(err)
	s.Require().NoError(blobWriter.Close())
	blob, err := sto.SetEncodedObject(blobObj)
	s.Require().NoError(err)

	tree := &object.Tree{Entries: []object.TreeEntry{
		{Name: "README", Mode: filemode.Regular, Hash: blob},
	}}
	treeObj := sto.NewEncodedObject()
	treeObj.SetType(plumbing.TreeObject)
	s.Require().NoError(tree.Encode(treeObj))
	treeHash, err := sto.SetEncodedObject(treeObj)
	s.Require().NoError(err)

	missingParent := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	commit := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Message:      "shallow",
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{missingParent},
	}
	commitObj := sto.NewEncodedObject()
	commitObj.SetType(plumbing.CommitObject)
	s.Require().NoError(commit.Encode(commitObj))
	commitHash, err := sto.SetEncodedObject(commitObj)
	s.Require().NoError(err)
	s.Require().NoError(sto.SetShallow([]plumbing.Hash{commitHash}))

	tag := &object.Tag{
		Name:       "v0.0.1",
		Tagger:     object.Signature{Name: "Test", Email: "t@t.com", When: when},
		TargetType: plumbing.CommitObject,
		Target:     commitHash,
		Message:    "test tag",
	}
	tagObj := sto.NewEncodedObject()
	tagObj.SetType(plumbing.TagObject)
	s.Require().NoError(tag.Encode(tagObj))
	tagHash, err := sto.SetEncodedObject(tagObj)
	s.Require().NoError(err)

	got, err := Objects(sto, []plumbing.Hash{tagHash}, nil)
	s.Require().NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	s.True(gotSet[tagHash], "tag must be included")
	s.True(gotSet[commitHash], "shallow commit must be included")
	s.True(gotSet[treeHash], "commit tree must be included")
	s.False(gotSet[missingParent], "missing parent beyond shallow boundary must not be walked")
}

// benchFixture opens the src-d/go-git fixture (2133 objects) and walks
// back from HEAD to find a commit ~10 commits earlier to use as the
// haves boundary.
func benchFixture(b *testing.B) (storer.EncodedObjectStorer, plumbing.Hash, plumbing.Hash) {
	b.Helper()
	dotgit, err := fixtures.ByURL("https://github.com/src-d/go-git.git").One().DotGit()
	if err != nil {
		b.Fatal(err)
	}
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	head := plumbing.NewHash("e8788ad9165781196e917292d6055cba1d78664e")
	c, err := object.GetCommit(sto, head)
	if err != nil {
		b.Fatal(err)
	}

	// Walk back ~10 first-parent commits to find a reasonable haves tip.
	for range 10 {
		if c.NumParents() == 0 {
			break
		}
		c, err = c.Parent(0)
		if err != nil {
			b.Fatal(err)
		}
	}

	return sto, head, c.Hash
}

func BenchmarkObjects(b *testing.B) {
	s, want, have := benchFixture(b)
	b.ResetTimer()
	for range b.N {
		_, err := Objects(s, []plumbing.Hash{want}, []plumbing.Hash{have})
		if err != nil {
			b.Fatal(err)
		}
	}
}
