package git

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

type logPathCommit struct {
	name    string
	parents []string
	files   map[string]string
}

// commitLogPathHistory commits each entry with exactly the files it lists and
// returns the hash of every commit by name.
func commitLogPathHistory(t *testing.T, commits []logPathCommit) (*Repository, map[string]plumbing.Hash) {
	t.Helper()

	fs := memfs.New()
	r, err := Init(memory.NewStorage(), WithWorkTree(fs))
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	hashes := make(map[string]plumbing.Hash, len(commits))
	for i, c := range commits {
		entries, err := fs.ReadDir("/")
		require.NoError(t, err)
		for _, e := range entries {
			require.NoError(t, util.RemoveAll(fs, e.Name()))
		}
		for path, content := range c.files {
			require.NoError(t, util.WriteFile(fs, path, []byte(content+"\n"), 0o644))
		}
		require.NoError(t, w.AddWithOptions(&AddOptions{All: true}))

		var parents []plumbing.Hash
		for _, p := range c.parents {
			parents = append(parents, hashes[p])
		}

		sig := &object.Signature{
			Name:  "go-git",
			Email: "go-git@example.com",
			When:  time.Date(2020, time.January, 1, 0, i, 0, 0, time.UTC),
		}
		hashes[c.name], err = w.Commit(c.name, &CommitOptions{
			Author:            sig,
			Committer:         sig,
			Parents:           parents,
			AllowEmptyCommits: true,
		})
		require.NoError(t, err)
	}

	return r, hashes
}

func TestLogPathSimplifiesMergesLikeGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		commits    []logPathCommit
		fileName   string
		pathFilter func(string) bool
		want       []string
	}{
		{
			name: "merge same as first parent hides the side branch",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"f": "1", "g": "1"}},
				{name: "S", parents: []string{"A"}, files: map[string]string{"f": "side", "g": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"f": "2", "g": "1"}},
				{name: "C", parents: []string{"B"}, files: map[string]string{"f": "2", "g": "2"}},
				{name: "M", parents: []string{"C", "S"}, files: map[string]string{"f": "2", "g": "2"}},
			},
			fileName: "f",
			want:     []string{"A", "B"},
		},
		{
			name: "merge same as second parent follows that parent alone",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"f": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"f": "tmp"}},
				{name: "C", parents: []string{"B"}, files: map[string]string{"f": "1"}},
				{name: "S", parents: []string{"A"}, files: map[string]string{"f": "side"}},
				{name: "M", parents: []string{"C", "S"}, files: map[string]string{"f": "side"}},
			},
			fileName: "f",
			want:     []string{"A", "S"},
		},
		{
			name: "merge same as both parents follows the first",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"f": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"f": "x"}},
				{name: "S", parents: []string{"A"}, files: map[string]string{"f": "x"}},
				{name: "M", parents: []string{"B", "S"}, files: map[string]string{"f": "x"}},
			},
			fileName: "f",
			want:     []string{"A", "B"},
		},
		{
			name: "merge that differs from every parent is listed and walks all of them",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"f": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"f": "2"}},
				{name: "S", parents: []string{"A"}, files: map[string]string{"f": "side"}},
				{name: "M", parents: []string{"B", "S"}, files: map[string]string{"f": "merged"}},
			},
			fileName: "f",
			want:     []string{"A", "B", "M", "S"},
		},
		{
			name: "commit that deletes the path is listed",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"f": "1", "g": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"g": "1"}},
				{name: "S", parents: []string{"A"}, files: map[string]string{"f": "1", "g": "side"}},
				{name: "M", parents: []string{"B", "S"}, files: map[string]string{"g": "side"}},
			},
			fileName: "f",
			want:     []string{"A", "B"},
		},
		{
			name: "root commit without the path is not listed",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"g": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"f": "1", "g": "1"}},
				{name: "S", parents: []string{"B"}, files: map[string]string{"f": "1", "g": "side"}},
				{name: "C", parents: []string{"B"}, files: map[string]string{"f": "2", "g": "1"}},
				{name: "M", parents: []string{"C", "S"}, files: map[string]string{"f": "2", "g": "side"}},
			},
			fileName: "f",
			want:     []string{"B", "C"},
		},
		{
			name: "path filter on a directory",
			commits: []logPathCommit{
				{name: "A", files: map[string]string{"dir/f": "1", "g": "1"}},
				{name: "S", parents: []string{"A"}, files: map[string]string{"dir/f": "1", "dir/h": "side", "g": "1"}},
				{name: "B", parents: []string{"A"}, files: map[string]string{"dir/f": "2", "g": "1"}},
				{name: "C", parents: []string{"B"}, files: map[string]string{"dir/f": "2", "g": "2"}},
				{name: "M", parents: []string{"C", "S"}, files: map[string]string{"dir/f": "2", "g": "2"}},
			},
			pathFilter: func(path string) bool { return strings.HasPrefix(path, "dir/") },
			want:       []string{"A", "B"},
		},
	}

	orders := []struct {
		name  string
		order LogOrder
	}{
		{"Default", LogOrderDefault},
		{"DFS", LogOrderDFS},
		{"DFSPost", LogOrderDFSPost},
		{"BSF", LogOrderBSF},
		{"CommitterTime", LogOrderCommitterTime},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, hashes := commitLogPathHistory(t, tc.commits)
			defer func() { _ = r.Close() }()

			names := make(map[plumbing.Hash]string, len(hashes))
			for name, h := range hashes {
				names[h] = name
			}

			for _, o := range orders {
				opts := &LogOptions{Order: o.order, PathFilter: tc.pathFilter}
				if tc.fileName != "" {
					opts.FileName = &tc.fileName
				}

				iter, err := r.Log(opts)
				require.NoError(t, err)

				var got []string
				require.NoError(t, iter.ForEach(func(c *object.Commit) error {
					got = append(got, names[c.Hash])
					return nil
				}))
				slices.Sort(got)

				require.Equal(t, tc.want, got, "order %s", o.name)
			}
		})
	}
}
