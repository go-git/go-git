package object

import (
	"context"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/diff"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestPatchByteContents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, new string
		insertion      bool
		binary         bool
		added, deleted int
	}{
		{name: "invalid UTF-8", old: "\xff\nsame\n", new: "\xfe\nsame\n", added: 1, deleted: 1},
		{name: "CRLF", old: "old\r\n", new: "new\r\n", added: 1, deleted: 1},
		{name: "no final newline", old: "old", new: "new", added: 1, deleted: 1},
		{name: "empty addition", insertion: true},
		{name: "empty modification", old: "old\n", deleted: 1},
		{name: "binary", old: "old\x00", new: "new\x00", binary: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			storage := memory.NewStorage()
			entry := func(content string) ChangeEntry {
				o := storage.NewEncodedObject()
				o.SetType(plumbing.BlobObject)
				w, err := o.Writer()
				require.NoError(t, err)
				_, err = w.Write([]byte(content))
				require.NoError(t, err)
				require.NoError(t, w.Close())
				hash, err := storage.SetEncodedObject(o)
				require.NoError(t, err)
				return ChangeEntry{
					Name: "file", Tree: &Tree{s: storage},
					TreeEntry: TreeEntry{Name: "file", Mode: filemode.Regular, Hash: hash},
				}
			}
			change := &Change{To: entry(tc.new)}
			if !tc.insertion {
				change.From = entry(tc.old)
			}
			patch, err := change.Patch()
			require.NoError(t, err)
			fp := patch.FilePatches()[0]
			require.Equal(t, tc.binary, fp.IsBinary())
			stats := patch.Stats()
			if tc.binary {
				require.Nil(t, fp.Diff())
				require.Contains(t, patch.String(), "Binary files")
				require.Empty(t, stats)
			} else {
				require.NotNil(t, fp.Diff())
				var oldContent, newContent []byte
				for hunk, err := range fp.Diff().Hunks(t.Context(), diff.HunkOptions{Context: int(^uint(0) >> 1)}) {
					require.NoError(t, err)
					for _, line := range hunk.Lines {
						if line.Operation != diff.Insert {
							oldContent = append(oldContent, line.Content...)
						}
						if line.Operation != diff.Delete {
							newContent = append(newContent, line.Content...)
						}
					}
				}
				require.Equal(t, tc.old, string(oldContent))
				require.Equal(t, tc.new, string(newContent))
				require.Equal(t, FileStats{{Name: "file", Addition: tc.added, Deletion: tc.deleted}}, stats)
				require.NotContains(t, patch.String(), "Binary files")
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = change.PatchContext(ctx)
			require.ErrorIs(t, err, ErrCanceled)
			if !tc.binary {
				_, err = filePatchWithContext(ctx, change)
				require.ErrorIs(t, err, ErrCanceled)
			}
		})
	}
}

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

func (s *PatchSuite) TestPatchWithSubmodule() {
	subDotgit, err := fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit()
	s.Require().NoError(err)
	storer := filesystem.NewStorage(subDotgit, cache.NewObjectLRUDefault())
	defer func() { _ = storer.Close() }()

	commit, err := GetCommit(storer, plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4"))
	s.Require().NoError(err)

	tree, err := commit.Tree()
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

	p, err := getPatch("", added)
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

	p, err = getPatch("", updated)
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
