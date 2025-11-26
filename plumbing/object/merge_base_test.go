package object

import (
	"fmt"
	"sort"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func alphabeticSortCommits(commits []*Commit) {
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].Hash.String() > commits[j].Hash.String()
	})
}

/*

The following tests consider this history having two root commits: V and W

V---o---M----AB----A---CD1--P---C--------S-------------------Q < master
               \         \ /            /                   /
                \         X            GQ1---G < feature   /
                 \       / \          /     /             /
W---o---N----o----B---CD2---o---D----o----GQ2------------o < dev

MergeBase
----------------------------
passed  merge-base
 M, N               Commits with unrelated history, have no merge-base
 A, B    AB         Regular merge-base between two commits
 A, A    A          The merge-commit between equal commits, is the same
 Q, N    N          The merge-commit between a commit an its ancestor, is the ancestor
 C, D    CD1, CD2   Cross merges causes more than one merge-base
 G, Q    GQ1, GQ2   Feature branches including merges, causes more than one merge-base

Independents
----------------------------
candidates           result
 A                    A           Only one commit returns it
 A, A, A              A           Repeated commits are ignored
 A, A, M, M, N        A, N        M is reachable from A, so it is not independent
 S, G, P              S, G        P is reachable from S, so it is not independent
 CD1, CD2, M, N       CD1, CD2    M and N are reachable from CD2, so they're not
 C, G, dev, M, N      C, G, dev   M and N are reachable from G, so they're not
 C, D, M, N           C, D        M and N are reachable from C, so they're not
 A, A^, A, N, N^      A, N        A^ and N^ are reachable from A and N
 A^^^, A^, A^^, A, N  A, N        A^^^, A^^ and A^ are reachable from A, so they're not

IsAncestor
----------------------------
passed   result
 A^^, A   true      Will be true if first is ancestor of the second
 M, G     true      True because it will also reach G from M crossing merge commits
 A, A     true      True if first and second are the same
 M, N     false     Commits with unrelated history, will return false
*/

func TestMergeBaseSuite(t *testing.T) {
	suite.Run(t, new(mergeBaseSuite))
}

type mergeBaseSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func (s *mergeBaseSuite) SetupSuite() {
	s.Fixture = fixtures.ByTag("merge-base").One()
	s.Storer = filesystem.NewStorage(s.Fixture.DotGit(), cache.NewObjectLRUDefault())
}

var revisionIndex = map[string]plumbing.Hash{
	"master":  plumbing.NewHash("dce0e0c20d701c3d260146e443d6b3b079505191"),
	"feature": plumbing.NewHash("d1b0093698e398d596ef94d646c4db37e8d1e970"),
	"dev":     plumbing.NewHash("25ca6c810c08482d61113fbcaaada38bb59093a8"),
	"M":       plumbing.NewHash("bb355b64e18386dbc3af63dfd09c015c44cbd9b6"),
	"N":       plumbing.NewHash("d64b894762ab5f09e2b155221b90c18bd0637236"),
	"A":       plumbing.NewHash("29740cfaf0c2ee4bb532dba9e80040ca738f367c"),
	"B":       plumbing.NewHash("2c84807970299ba98951c65fe81ebbaac01030f0"),
	"AB":      plumbing.NewHash("31a7e081a28f149ee98ffd13ba1a6d841a5f46fd"),
	"P":       plumbing.NewHash("ff84393134864cf9d3a9853a81bde81778bd5805"),
	"C":       plumbing.NewHash("8b72fabdc4222c3ff965bc310ded788c601c50ed"),
	"D":       plumbing.NewHash("14777cf3e209334592fbfd0b878f6868394db836"),
	"CD1":     plumbing.NewHash("4709e13a3cbb300c2b8a917effda776e1b8955c7"),
	"CD2":     plumbing.NewHash("38468e274e91e50ffb637b88a1954ab6193fe974"),
	"S":       plumbing.NewHash("628f1a42b70380ed05734bf01b468b46206ef1ea"),
	"G":       plumbing.NewHash("d1b0093698e398d596ef94d646c4db37e8d1e970"),
	"Q":       plumbing.NewHash("dce0e0c20d701c3d260146e443d6b3b079505191"),
	"GQ1":     plumbing.NewHash("ccaaa99c21dad7e9f392c36ae8cb72dc63bed458"),
	"GQ2":     plumbing.NewHash("806824d4778e94fe7c3244e92a9cd07090c9ab54"),
	"A^":      plumbing.NewHash("31a7e081a28f149ee98ffd13ba1a6d841a5f46fd"),
	"A^^":     plumbing.NewHash("bb355b64e18386dbc3af63dfd09c015c44cbd9b6"),
	"A^^^":    plumbing.NewHash("8d08dd1388b82dd354cb43918d83da86c76b0978"),
	"N^":      plumbing.NewHash("b6e1fc8dad4f1068fb42774ec5fc65c065b2c312"),
}

func (s *mergeBaseSuite) commitsFromRevs(revs []string) ([]*Commit, error) {
	var commits []*Commit
	for _, rev := range revs {
		hash, ok := revisionIndex[rev]
		if !ok {
			return nil, fmt.Errorf("Revision not found '%s'", rev)
		}

		commits = append(commits, s.commit(hash))
	}

	return commits, nil
}

// AssertMergeBase validates that the merge-base of the passed revs,
// matches the expected result
func (s *mergeBaseSuite) AssertMergeBase(revs, expectedRevs []string) {
	s.Len(revs, 2)

	commits, err := s.commitsFromRevs(revs)
	s.NoError(err)

	results, err := commits[0].MergeBase(commits[1])
	s.NoError(err)

	expected, err := s.commitsFromRevs(expectedRevs)
	s.NoError(err)

	s.Len(results, len(expected))

	alphabeticSortCommits(results)
	alphabeticSortCommits(expected)
	for i, commit := range results {
		s.Equal(expected[i].Hash.String(), commit.Hash.String())
	}
}

// AssertIndependents validates the independent commits of the passed list
func (s *mergeBaseSuite) AssertIndependents(revs, expectedRevs []string) {
	commits, err := s.commitsFromRevs(revs)
	s.NoError(err)

	results, err := Independents(commits)
	s.NoError(err)

	expected, err := s.commitsFromRevs(expectedRevs)
	s.NoError(err)

	s.Len(results, len(expected))

	alphabeticSortCommits(results)
	alphabeticSortCommits(expected)
	for i, commit := range results {
		s.Equal(expected[i].Hash.String(), commit.Hash.String())
	}
}

// AssertAncestor validates if the first rev is ancestor of the second one
func (s *mergeBaseSuite) AssertAncestor(revs []string, shouldBeAncestor bool) {
	s.Len(revs, 2)

	commits, err := s.commitsFromRevs(revs)
	s.NoError(err)

	isAncestor, err := commits[0].IsAncestor(commits[1])
	s.NoError(err)
	s.Equal(shouldBeAncestor, isAncestor)
}

// TestNoAncestorsWhenNoCommonHistory validates that merge-base returns no commits
// when there is no common history (M, N -> none)
func (s *mergeBaseSuite) TestNoAncestorsWhenNoCommonHistory() {
	revs := []string{"M", "N"}
	nothing := []string{}
	s.AssertMergeBase(revs, nothing)
}

// TestCommonAncestorInMergedOrphans validates that merge-base returns a common
// ancestor in orphan branches when they where merged (A, B -> AB)
func (s *mergeBaseSuite) TestCommonAncestorInMergedOrphans() {
	revs := []string{"A", "B"}
	expectedRevs := []string{"AB"}
	s.AssertMergeBase(revs, expectedRevs)
}

// TestMergeBaseWithSelf validates that merge-base between equal commits, returns
// the same commit (A, A -> A)
func (s *mergeBaseSuite) TestMergeBaseWithSelf() {
	revs := []string{"A", "A"}
	expectedRevs := []string{"A"}
	s.AssertMergeBase(revs, expectedRevs)
}

// TestMergeBaseWithAncestor validates that merge-base between a commit an its
// ancestor returns the ancestor (Q, N -> N)
func (s *mergeBaseSuite) TestMergeBaseWithAncestor() {
	revs := []string{"Q", "N"}
	expectedRevs := []string{"N"}
	s.AssertMergeBase(revs, expectedRevs)
}

// TestDoubleCommonAncestorInCrossMerge validates that merge-base returns two
// common ancestors when there are cross merges (C, D -> CD1, CD2)
func (s *mergeBaseSuite) TestDoubleCommonAncestorInCrossMerge() {
	revs := []string{"C", "D"}
	expectedRevs := []string{"CD1", "CD2"}
	s.AssertMergeBase(revs, expectedRevs)
}

// TestDoubleCommonInSubFeatureBranches validates that merge-base returns two
// common ancestors when two branches where partially merged (G, Q -> GQ1, GQ2)
func (s *mergeBaseSuite) TestDoubleCommonInSubFeatureBranches() {
	revs := []string{"G", "Q"}
	expectedRevs := []string{"GQ1", "GQ2"}
	s.AssertMergeBase(revs, expectedRevs)
}

// TestIndependentOnlyOne validates that Independents for one commit returns
// that same commit (A -> A)
func (s *mergeBaseSuite) TestIndependentOnlyOne() {
	revs := []string{"A"}
	expectedRevs := []string{"A"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentOnlyRepeated validates that Independents for one repeated commit
// returns that same commit (A, A, A -> A)
func (s *mergeBaseSuite) TestIndependentOnlyRepeated() {
	revs := []string{"A", "A", "A"}
	expectedRevs := []string{"A"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentWithRepeatedAncestors validates that Independents works well
// when there are repeated ancestors (A, A, M, M, N -> A, N)
func (s *mergeBaseSuite) TestIndependentWithRepeatedAncestors() {
	revs := []string{"A", "A", "M", "M", "N"}
	expectedRevs := []string{"A", "N"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentBeyondShortcut validates that Independents does not stop walking
// in all paths when one of them is known (S, G, P -> S, G)
func (s *mergeBaseSuite) TestIndependentBeyondShortcut() {
	revs := []string{"S", "G", "P"}
	expectedRevs := []string{"S", "G"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentBeyondShortcutBis validates that Independents does not stop walking
// in all paths when one of them is known (CD1, CD2, M, N -> CD1, CD2)
func (s *mergeBaseSuite) TestIndependentBeyondShortcutBis() {
	revs := []string{"CD1", "CD2", "M", "N"}
	expectedRevs := []string{"CD1", "CD2"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentWithPairOfAncestors validates that Independents excluded all
// the ancestors (C, D, M, N -> C, D)
func (s *mergeBaseSuite) TestIndependentWithPairOfAncestors() {
	revs := []string{"C", "D", "M", "N"}
	expectedRevs := []string{"C", "D"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentAcrossCrossMerges validates that Independents works well
// along cross merges (C, G, dev, M -> C, G, dev)
func (s *mergeBaseSuite) TestIndependentAcrossCrossMerges() {
	revs := []string{"C", "G", "dev", "M", "N"}
	expectedRevs := []string{"C", "G", "dev"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentChangingOrderRepetition validates that Independents works well
// when the order and repetition is tricky (A, A^, A, N, N^ -> A, N)
func (s *mergeBaseSuite) TestIndependentChangingOrderRepetition() {
	revs := []string{"A", "A^", "A", "N", "N^"}
	expectedRevs := []string{"A", "N"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestIndependentChangingOrder validates that Independents works well
// when the order is tricky (A^^^, A^, A^^, A, N -> A, N)
func (s *mergeBaseSuite) TestIndependentChangingOrder() {
	revs := []string{"A^^^", "A^", "A^^", "A", "N"}
	expectedRevs := []string{"A", "N"}
	s.AssertIndependents(revs, expectedRevs)
}

// TestAncestor validates that IsAncestor returns true if walking from first
// commit, through its parents, it can be reached the second ( A^^, A -> true )
func (s *mergeBaseSuite) TestAncestor() {
	revs := []string{"A^^", "A"}
	s.AssertAncestor(revs, true)

	revs = []string{"A", "A^^"}
	s.AssertAncestor(revs, false)
}

// TestAncestorBeyondMerges validates that IsAncestor returns true also if first can be
// be reached from first one even crossing merge commits in between ( M, G -> true )
func (s *mergeBaseSuite) TestAncestorBeyondMerges() {
	revs := []string{"M", "G"}
	s.AssertAncestor(revs, true)

	revs = []string{"G", "M"}
	s.AssertAncestor(revs, false)
}

// TestAncestorSame validates that IsAncestor returns both are the same ( A, A -> true )
func (s *mergeBaseSuite) TestAncestorSame() {
	revs := []string{"A", "A"}
	s.AssertAncestor(revs, true)
}

// TestAncestorUnrelated validates that IsAncestor returns false when the passed commits
// does not share any history, no matter the order used ( M, N -> false )
func (s *mergeBaseSuite) TestAncestorUnrelated() {
	revs := []string{"M", "N"}
	s.AssertAncestor(revs, false)

	revs = []string{"N", "M"}
	s.AssertAncestor(revs, false)
}
