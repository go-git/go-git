package revlist

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
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

type delegatedObjectStorer struct {
	storer.EncodedObjectStorer
	called bool
	wants  []plumbing.Hash
	haves  []plumbing.Hash
	result []plumbing.Hash
}

func (s *delegatedObjectStorer) RevListObjects(wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	s.called = true
	s.wants = append([]plumbing.Hash(nil), wants...)
	s.haves = append([]plumbing.Hash(nil), haves...)
	return append([]plumbing.Hash(nil), s.result...), nil
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

func (s *RevListSuite) TestRevListObjects_DelegatesToObjectWalker() {
	want := plumbing.NewHash(initialCommit)
	have := plumbing.NewHash(secondCommit)
	expected := []plumbing.Hash{plumbing.NewHash(someCommit)}

	sto := &delegatedObjectStorer{
		EncodedObjectStorer: memory.NewStorage(),
		result:              expected,
	}

	got, err := Objects(sto, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.Require().NoError(err)

	s.True(sto.called)
	s.Equal([]plumbing.Hash{want}, sto.wants)
	s.Equal([]plumbing.Hash{have}, sto.haves)
	s.Equal(expected, got)
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

var commitCounter int

func testMakeTree(t *testing.T, s storer.EncodedObjectStorer, entries []object.TreeEntry) plumbing.Hash {
	t.Helper()
	tree := &object.Tree{Entries: entries}
	obj := s.NewEncodedObject()
	obj.SetType(plumbing.TreeObject)
	require.NoError(t, tree.Encode(obj))
	hash, err := s.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

func testMakeCommit(t *testing.T, s storer.EncodedObjectStorer, treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	commitCounter++
	when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(commitCounter) * time.Second)
	return testMakeCommitAt(t, s, treeHash, when, parents...)
}

func testMakeCommitAt(t *testing.T, s storer.EncodedObjectStorer, treeHash plumbing.Hash, when time.Time, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	c := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Committer:    object.Signature{Name: "Test", Email: "t@t.com", When: when},
		Message:      "test",
		TreeHash:     treeHash,
		ParentHashes: parents,
	}
	obj := s.NewEncodedObject()
	obj.SetType(plumbing.CommitObject)
	require.NoError(t, c.Encode(obj))
	hash, err := s.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
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

func (s *RevListSuite) makeTree(entries []object.TreeEntry) plumbing.Hash {
	return testMakeTree(s.T(), s.Storer, entries)
}

func (s *RevListSuite) makeCommit(treeHash plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	return testMakeCommit(s.T(), s.Storer, treeHash, parents...)
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

func (s *RevListSuite) TestRevListObjects_ReintroducedBlobInHaveAncestor() {
	// A blob exists in a haves ancestor (not the tip), gets removed in
	// the haves tip, then reintroduced in wants. We verify our output
	// matches git rev-list --objects exactly.
	//
	// Use an on-disk fixture so we can compare against git rev-list.
	dotgit, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	t := s.T()
	license := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	gitignore := plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")

	baseTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: gitignore},
		{Name: "LICENSE", Mode: filemode.Regular, Hash: license},
	})
	base := testMakeCommit(t, sto, baseTree)

	haveTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: gitignore},
	})
	have := testMakeCommit(t, sto, haveTree, base)

	wantTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: gitignore},
		{Name: "LICENSE", Mode: filemode.Regular, Hash: license},
	})
	want := testMakeCommit(t, sto, wantTree, have)

	got, err := Objects(sto, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.NoError(err)

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	// Compare against git rev-list --objects.
	gitSet := gitRevListObjects(s.T(), dotgit.Root(), want, have)

	s.Equal(gitSet, gotSet, "Objects output must match git rev-list --objects")
}

// gitRevListObjects runs git rev-list --objects want ^have against the
// given git directory and returns the set of object hashes.
func gitRevListObjects(t *testing.T, gitDir string, want, have plumbing.Hash) map[plumbing.Hash]bool {
	t.Helper()
	set, err := tryGitRevListObjects(t, gitDir, want, have)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// tryGitRevListObjects is the error-returning variant of
// gitRevListObjects for tests that need to assert on git failures.
func tryGitRevListObjects(t *testing.T, gitDir string, want, have plumbing.Hash) (map[plumbing.Hash]bool, error) {
	t.Helper()
	args := []string{"--git-dir", gitDir, "rev-list", "--objects", want.String(), "^" + have.String()}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git rev-list failed: %s\n%s", err, string(out))
	}

	set := make(map[plumbing.Hash]bool)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// git rev-list --objects outputs "hash path" for tree entries;
		// we only need the hash (first field).
		fields := strings.Fields(line)
		set[plumbing.NewHash(fields[0])] = true
	}
	return set, nil
}

func (s *RevListSuite) TestRevListObjects_MissingHaveAncestorIsTolerated() {
	// A haves commit references a parent that doesn't exist locally.
	// Matching git's behavior, missing haves ancestors are silently
	// skipped — the remote may advertise refs whose ancestors we
	// don't have locally. Verified against git rev-list --objects.
	dotgit, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	t := s.T()

	haveTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")},
	})
	missingAncestor := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	have := testMakeCommit(t, sto, haveTree, missingAncestor)

	wantTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: "LICENSE", Mode: filemode.Regular, Hash: plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")},
	})
	want := testMakeCommit(t, sto, wantTree, have)

	got, err := Objects(sto, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.NoError(err, "missing haves ancestors should be tolerated")

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	gitSet := gitRevListObjects(t, dotgit.Root(), want, have)
	s.Equal(gitSet, gotSet, "Objects output must match git rev-list --objects")
}

func (s *RevListSuite) TestRevListObjects_MissingWantAncestorErrors() {
	// A want commit references a parent that doesn't exist locally, and
	// the missing parent is not reachable from any have (so it is not a
	// shallow or haves-boundary case). git rev-list --objects errors in
	// this case with "fatal: cannot simplify commit ... (because of
	// <missing>)"; Objects() should match that behavior.
	dotgit, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	t := s.T()

	// Unrelated have — no shared ancestry with want, so it cannot
	// "cover" the missing ancestor.
	haveTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: "have", Mode: filemode.Regular, Hash: plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")},
	})
	have := testMakeCommit(t, sto, haveTree)

	// Want with a missing parent commit, not reachable from have.
	missingAncestor := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	wantTree := testMakeTree(t, sto, []object.TreeEntry{
		{Name: "want", Mode: filemode.Regular, Hash: plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")},
	})
	want := testMakeCommit(t, sto, wantTree, missingAncestor)

	// git rev-list must error on this case — confirm the premise.
	_, gitErr := tryGitRevListObjects(t, dotgit.Root(), want, have)
	s.Require().Error(gitErr, "git rev-list must error on missing want ancestor (otherwise this test's premise is wrong)")

	// Objects() should match git's behavior and return an error.
	_, err = Objects(sto, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.Error(err, "Objects must error on missing want ancestor to match git rev-list")
}

func (s *RevListSuite) TestRevListObjects_MissingWantParentTreeReturnsError() {
	missingTree := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	parent := s.makeCommit(missingTree)

	wantTree := s.makeTree([]object.TreeEntry{
		{Name: ".gitignore", Mode: filemode.Regular, Hash: plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")},
	})
	want := s.makeCommit(wantTree, parent)

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

func (s *RevListSuite) TestRevListObjects_ClockSkewedHaveAncestorMissingParentTolerated() {
	// A commit reachable from both wants and haves has a missing parent.
	// Due to clock skew, that commit has a later timestamp than the
	// haves tip and would be popped from the priority queue first.
	// Ensure the missing parent is tolerated (matching Git behavior).
	//
	//   H (have, T=1) ──→ A (T=100, clock skew) ──→ missing
	//   W (want, T=10) ─→ B (T=5) ──→ A
	license := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	gitignore := plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")

	aTree := s.makeTree([]object.TreeEntry{
		{Name: "shared", Mode: filemode.Regular, Hash: license},
	})
	missingParent := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	a := s.makeCommitAt(aTree, time.Date(2024, 1, 1, 1, 40, 0, 0, time.UTC), missingParent)

	haveTree := s.makeTree([]object.TreeEntry{
		{Name: "shared", Mode: filemode.Regular, Hash: license},
	})
	have := s.makeCommitAt(haveTree, time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC), a)

	bTree := s.makeTree([]object.TreeEntry{
		{Name: "extra", Mode: filemode.Regular, Hash: gitignore},
		{Name: "shared", Mode: filemode.Regular, Hash: license},
	})
	b := s.makeCommitAt(bTree, time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC), a)

	wantTree := s.makeTree([]object.TreeEntry{
		{Name: "unique", Mode: filemode.Regular, Hash: gitignore},
	})
	want := s.makeCommitAt(wantTree, time.Date(2024, 1, 1, 0, 10, 0, 0, time.UTC), b)

	got, err := Objects(s.Storer, []plumbing.Hash{want}, []plumbing.Hash{have})
	s.NoError(err, "missing parent of clock-skewed haves ancestor must be tolerated")

	gotSet := make(map[plumbing.Hash]bool, len(got))
	for _, h := range got {
		gotSet[h] = true
	}

	s.True(gotSet[want], "want tip must be included")
	s.False(gotSet[a], "shared ancestor must not be included")
	s.False(gotSet[have], "have must not be included")
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
