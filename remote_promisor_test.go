package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestRecordPromisor covers the config a filtered fetch has to leave behind.
//
// Both keys have to land together. promisor alone is the state behind the
// reported fetch failures: git accepts that objects are absent but, with no
// partialclonefilter to reapply, fetches unfiltered, and index-pack then dies
// resolving deltas against bases the pack assumed were local.
func TestRecordPromisor(t *testing.T) {
	t.Parallel()

	newRepo := func(t *testing.T) *Repository {
		t.Helper()
		r, err := Init(memory.NewStorage())
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })
		_, err = r.CreateRemote(&config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{"https://github.com/go-git/go-git.git"},
		})
		require.NoError(t, err)
		return r
	}

	t.Run("records both keys and the format version", func(t *testing.T) {
		t.Parallel()

		r := newRepo(t)
		rem, err := r.Remote(DefaultRemoteName)
		require.NoError(t, err)

		require.NoError(t, rem.recordPromisor(packp.FilterBlobNone()))

		cfg, err := r.Config()
		require.NoError(t, err)
		assert.True(t, cfg.Remotes[DefaultRemoteName].Promisor)
		assert.Equal(t, "blob:none", cfg.Remotes[DefaultRemoteName].PartialCloneFilter)
		assert.EqualValues(t, formatcfg.Version1, cfg.Core.RepositoryFormatVersion,
			"partial clone is a format extension, so extensions have to be permitted")

		// The in-memory view has to agree with what was stored.
		assert.True(t, rem.Config().Promisor)
		assert.Equal(t, "blob:none", rem.Config().PartialCloneFilter)
	})

	t.Run("updates the filter when it changes", func(t *testing.T) {
		t.Parallel()

		r := newRepo(t)
		rem, err := r.Remote(DefaultRemoteName)
		require.NoError(t, err)

		require.NoError(t, rem.recordPromisor(packp.FilterBlobNone()))
		require.NoError(t, rem.recordPromisor(packp.FilterTreeDepth(0)))

		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Equal(t, "tree:0", cfg.Remotes[DefaultRemoteName].PartialCloneFilter)
	})

	t.Run("records nothing for an unconfigured remote", func(t *testing.T) {
		t.Parallel()

		r, err := Init(memory.NewStorage())
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		// An anonymous URL fetch has no remote section to write to, and git
		// leaves the configured remotes alone in that case too.
		rem := NewRemote(r.Storer, &config.RemoteConfig{
			Name: "",
			URLs: []string{"https://github.com/go-git/go-git.git"},
		})
		require.NoError(t, rem.recordPromisor(packp.FilterBlobNone()))

		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Empty(t, cfg.Remotes)
	})
}
