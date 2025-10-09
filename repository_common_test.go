package git

import (
	"testing"
	"time"

	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func forEachFormat(t *testing.T, fnT func(*testing.T, formatcfg.ObjectFormat)) {
	formats := []struct {
		name   string
		format formatcfg.ObjectFormat
	}{
		{name: "default"},
		{name: "sha1", format: formatcfg.SHA1},
		{name: "sha256", format: formatcfg.SHA256},
	}

	for _, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			fnT(t, f.format)
		})
	}
}

func createCommit(t *testing.T, r *Repository) plumbing.Hash {
	// Create a commit so there is a HEAD to check
	wt, err := r.Worktree()
	require.NoError(t, err)

	f, err := wt.Filesystem.Create("foo.txt")
	require.NoError(t, err)

	defer f.Close()

	_, err = f.Write([]byte("foo text"))
	require.NoError(t, err)

	_, err = wt.Add("foo.txt")
	require.NoError(t, err)

	author := object.Signature{
		Name:  "go-git",
		Email: "go-git@fake.local",
		When:  time.Now(),
	}

	h, err := wt.Commit("test commit message", &CommitOptions{
		All:               true,
		Author:            &author,
		Committer:         &author,
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)
	return h
}
