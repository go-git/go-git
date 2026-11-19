package loader

import (
	"io"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestDefaultLoadsAbsolutePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	repoPath := filepath.Join(dir, "repo")
	st := filesystem.NewStorage(osfs.New(filepath.Join(repoPath, ".git")), nil)
	defer func() { _ = st.Close() }()
	require.NoError(t, st.Init())
	cfg, err := st.Config()
	require.NoError(t, err)
	require.NoError(t, st.SetConfig(cfg))

	sto, err := Default().Load(&url.URL{Path: repoPath})
	require.NoError(t, err)
	defer func() {
		if closer, ok := sto.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	assert.NotNil(t, sto)
}

func TestDefaultMissingRepository(t *testing.T) {
	t.Parallel()

	_, err := Default().Load(&url.URL{Path: filepath.Join(t.TempDir(), "missing")})
	assert.ErrorIs(t, err, transport.ErrRepositoryNotFound)
}

func TestDefaultIsShared(t *testing.T) {
	t.Parallel()

	assert.Same(t, Default(), Default())
}
