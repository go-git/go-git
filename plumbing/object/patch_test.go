package object

import (
	"context"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
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
	storer := filesystem.NewStorage(
		fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit(), cache.NewObjectLRUDefault())

	commit, err := GetCommit(storer, plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4"))
	s.NoError(err)

	tree, err := commit.Tree()
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

	p, err := getPatch("", ch)
	s.NoError(err)
	s.NotNil(p)
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

// modifyChange returns a Change representing a modification to a known file
// in the go-git fixture, suitable for exercising filePatchWithContext.
func (s *PatchSuite) modifyChange() *Change {
	path := "utils/difftree/difftree.go"
	name := "difftree.go"
	mode := filemode.Regular
	fromBlob := plumbing.NewHash("05f583ace3a9a078d8150905a53a4d82567f125f")
	fromTree := plumbing.NewHash("b1f01b730b855c82431918cb338ad47ed558999b")
	toBlob := plumbing.NewHash("de927fad935d172929aacf20e71f3bf0b91dd6f9")
	toTree := plumbing.NewHash("8b0af31d2544acb5c4f3816a602f11418cbd126e")
	return &Change{
		From: ChangeEntry{
			Name:      path,
			Tree:      s.tree(fromTree),
			TreeEntry: TreeEntry{Name: name, Mode: mode, Hash: fromBlob},
		},
		To: ChangeEntry{
			Name:      path,
			Tree:      s.tree(toTree),
			TreeEntry: TreeEntry{Name: name, Mode: mode, Hash: toBlob},
		},
	}
}

// TestPatchContextDeadlineExpired verifies that filePatchWithContext returns
// ErrCanceled when the context deadline has already expired before the call.
func (s *PatchSuite) TestPatchContextDeadlineExpired() {
	change := s.modifyChange()

	// Deadline already in the past — filePatchWithContext must detect this
	// immediately and return ErrCanceled without performing any diff work.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	fp, err := filePatchWithContext(ctx, change)
	s.Nil(fp)
	s.ErrorIs(err, ErrCanceled)
}

// TestPatchContextWithActiveDeadline verifies that filePatchWithContext
// succeeds and returns a non-empty patch when given a context with ample time
// remaining, exercising the diff.DoWithTimeout code path.
func (s *PatchSuite) TestPatchContextWithActiveDeadline() {
	change := s.modifyChange()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp, err := filePatchWithContext(ctx, change)
	s.NoError(err)
	s.NotNil(fp)
	s.NotEmpty(fp.Chunks())
}

// TestPatchContextWithoutDeadline verifies that filePatchWithContext
// succeeds and returns a non-empty patch when given a context without
// deadline, exercising the diff.Do code path.
func (s *PatchSuite) TestPatchContextWithoutDeadline() {
	change := s.modifyChange()

	fp, err := filePatchWithContext(context.TODO(), change)
	s.NoError(err)
	s.NotNil(fp)
	s.NotEmpty(fp.Chunks())
}
