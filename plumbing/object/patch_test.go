package object

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type PatchSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func TestPatchSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PatchSuite))
}

func (s *PatchSuite) TestStatsWithSubmodules() {
	subDotgit, err := fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit()
	s.Require().NoError(err)
	storer := filesystem.NewStorage(subDotgit, cache.NewObjectLRUDefault())
	defer func() { _ = storer.Close() }()

	commit, err := GetCommit(s.T().Context(), storer, plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4"))
	s.NoError(err)

	tree, err := commit.Tree(s.T().Context())
	s.NoError(err)

	e, err := tree.entry("basic")
	s.NoError(err)

	ch := &Change{
		From: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: *e,
		},
		To: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: *e,
		},
	}

	p, err := getPatch(s.T().Context(), "", ch)
	s.NoError(err)
	s.NotNil(p)
}

func (s *PatchSuite) TestPatchWithSubmodule() {
	subDotgit, err := fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit()
	s.Require().NoError(err)
	storer := filesystem.NewStorage(subDotgit, cache.NewObjectLRUDefault())
	defer func() { _ = storer.Close() }()

	commit, err := GetCommit(s.T().Context(), storer, plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4"))
	s.Require().NoError(err)

	tree, err := commit.Tree(s.T().Context())
	s.Require().NoError(err)

	e, err := tree.entry("basic")
	s.Require().NoError(err)

	// Adding a submodule (gitlink) must be reflected in the patch as a
	// "Subproject commit <hash>" line, mirroring the output of `git diff`.
	added := &Change{
		To: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: *e,
		},
	}

	p, err := getPatch(s.T().Context(), "", added)
	s.Require().NoError(err)

	got := p.String()
	s.Contains(got, "diff --git a/basic b/basic")
	s.Contains(got, "new file mode 160000")
	s.Contains(got, "+Subproject commit 6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	// Updating the commit a submodule points to must produce a deletion of the
	// old commit line and an addition of the new one.
	other, err := tree.entry("itself")
	s.Require().NoError(err)

	updated := &Change{
		From: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: *e,
		},
		To: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: TreeEntry{Name: "basic", Mode: other.Mode, Hash: other.Hash},
		},
	}

	p, err = getPatch(s.T().Context(), "", updated)
	s.Require().NoError(err)

	got = p.String()
	s.Contains(got, "-Subproject commit 6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	s.Contains(got, "+Subproject commit 47770b26e71b0f69c0ecd494b1066f8d1da4fc03")
}

func (s *PatchSuite) TestFileStatsString() {
	testCases := []struct {
		description string
		input       FileStats
		expected    string
	}{
		{
			description: "no files changed",
			input:       []FileStat{},
			expected:    "",
		},
		{
			description: "one file touched - no changes",
			input: []FileStat{
				{
					Name: "file1",
				},
			},
			expected: " file1 | 0 \n",
		},
		{
			description: "one file changed",
			input: []FileStat{
				{
					Name:     "file1",
					Addition: 1,
				},
			},
			expected: " file1 | 1 +\n",
		},
		{
			description: "one file changed with one addition and one deletion",
			input: []FileStat{
				{
					Name:     ".github/workflows/git.yml",
					Addition: 1,
					Deletion: 1,
				},
			},
			expected: " .github/workflows/git.yml | 2 +-\n",
		},
		{
			description: "two files changed",
			input: []FileStat{
				{
					Name:     ".github/workflows/git.yml",
					Addition: 1,
					Deletion: 1,
				},
				{
					Name:     "cli/go-git/go.mod",
					Addition: 4,
					Deletion: 4,
				},
			},
			expected: " .github/workflows/git.yml | 2 +-\n cli/go-git/go.mod         | 8 ++++----\n",
		},
		{
			description: "three files changed",
			input: []FileStat{
				{
					Name:     ".github/workflows/git.yml",
					Addition: 3,
					Deletion: 3,
				},
				{
					Name:     "worktree.go",
					Addition: 107,
				},
				{
					Name:     "worktree_test.go",
					Addition: 75,
				},
			},
			expected: " .github/workflows/git.yml |   6 +++---\n" +
				" worktree.go               | 107 +++++++++++++++++++++++++++++++++++++++++++++++++++++\n" +
				" worktree_test.go          |  75 +++++++++++++++++++++++++++++++++++++++++++++++++++++\n",
		},
		{
			description: "three files changed with deletions and additions",
			input: []FileStat{
				{
					Name:     ".github/workflows/git.yml",
					Addition: 3,
					Deletion: 3,
				},
				{
					Name:     "worktree.go",
					Addition: 107,
					Deletion: 217,
				},
				{
					Name:     "worktree_test.go",
					Addition: 75,
					Deletion: 275,
				},
			},
			expected: " .github/workflows/git.yml |   6 +++---\n" +
				" worktree.go               | 324 ++++++++++++++++++-----------------------------------\n" +
				" worktree_test.go          | 350 ++++++++++++-----------------------------------------\n",
		},
	}

	for _, tc := range testCases {
		s.T().Log("Executing test cases:", tc.description)
		s.Equal(tc.expected, printStat(tc.input))
	}
}
