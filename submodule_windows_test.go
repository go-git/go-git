//go:build windows

package git

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestSubmoduleWindowsAbsoluteURLNotJoined verifies that absolute
// Windows paths in a submodule URL are recognised as absolute and
// therefore skip the relative-URL resolution branch. `path.IsAbs`
// alone returns false for `C:\…` and `\\server\share\…`; the
// production code pairs it with `filepath.IsAbs` so those cases
// don't get wrongly joined onto the superproject's remote URL.
func TestSubmoduleWindowsAbsoluteURLNotJoined(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		`C:\path\to\submodule`,
		`\\server\share\submodule`,
	} {
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			parent := &Repository{
				Storer: memory.NewStorage(),
				wt:     memfs.New(),
			}
			cfg, err := parent.Config()
			require.NoError(t, err)
			cfg.Remotes["origin"] = &config.RemoteConfig{
				Name: "origin",
				URLs: []string{"file:///parent/origin"},
			}
			require.NoError(t, parent.Storer.SetConfig(cfg))

			sub := &Submodule{
				initialized: true,
				w:           &Worktree{filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()), r: parent},
				c: &config.Submodule{
					Name: "child",
					Path: "child",
					URL:  url,
				},
			}

			subRepo, err := sub.Repository()
			require.NoError(t, err)
			defer func() { _ = subRepo.Close() }()

			remotes, err := subRepo.Remotes()
			require.NoError(t, err)
			require.Len(t, remotes, 1)

			got := remotes[0].Config().URLs[0]
			require.NotContains(t, got, "/parent/origin",
				"absolute Windows submodule URL was wrongly joined onto the parent's remote: %q", got)
		})
	}
}
