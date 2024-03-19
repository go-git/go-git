package object

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type PatchSuite struct {
	BaseObjectsSuite
}

var _ = Suite(&PatchSuite{})

func (s *PatchSuite) TestStatsWithSubmodules(c *C) {
	storer := filesystem.NewStorage(
		fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit(), cache.NewObjectLRUDefault())

	commit, err := GetCommit(storer, plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4"))
	c.Assert(err, IsNil)

	tree, err := commit.Tree()
	c.Assert(err, IsNil)

	e, err := tree.entry("basic")
	c.Assert(err, IsNil)

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

	p, err := getPatch("", ch)
	c.Assert(err, IsNil)
	c.Assert(p, NotNil)
}

func (s *PatchSuite) TestFileStatsString(c *C) {
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
		c.Log("Executing test cases:", tc.description)
		c.Assert(printStat(tc.input), Equals, tc.expected)
	}
}
