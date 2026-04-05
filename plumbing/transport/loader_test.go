package transport

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestFilesystemLoader_Load(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	repoPath := filepath.Join(dir, "repo.git")
	st := filesystem.NewStorage(osfs.New(repoPath), nil)
	require.NoError(t, st.Init())
	cfg, err := st.Config()
	require.NoError(t, err)
	cfg.Core.IsBare = true
	require.NoError(t, st.SetConfig(cfg))

	loader := NewFilesystemLoader(osfs.New(dir), false)

	u := &url.URL{Path: "repo"}
	sto, err := loader.Load(u)
	require.NoError(t, err)
	assert.NotNil(t, sto)
}

func TestFilesystemLoader_LoadBare(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	st := filesystem.NewStorage(osfs.New(filepath.Join(dir, "bare.git")), nil)
	require.NoError(t, st.Init())
	cfg, err := st.Config()
	require.NoError(t, err)
	cfg.Core.IsBare = true
	require.NoError(t, st.SetConfig(cfg))

	loader := NewFilesystemLoader(osfs.New(dir), false)

	u := &url.URL{Path: "bare.git"}
	sto, err := loader.Load(u)
	require.NoError(t, err)
	assert.NotNil(t, sto)
}

func TestFilesystemLoader_LoadNonExistent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	loader := NewFilesystemLoader(osfs.New(dir), false)

	u := &url.URL{Path: "does-not-exist"}
	sto, err := loader.Load(u)
	assert.ErrorIs(t, err, ErrRepositoryNotFound)
	assert.Nil(t, sto)
}

func TestMapLoader_Load(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	loader := MapLoader{"/test": st}

	u := &url.URL{Path: "/test"}
	sto, err := loader.Load(u)
	require.NoError(t, err)
	assert.Equal(t, st, sto)
}

func TestMapLoader_LoadNotFound(t *testing.T) {
	t.Parallel()

	loader := MapLoader{}
	u := &url.URL{Path: "/missing"}
	sto, err := loader.Load(u)
	assert.ErrorIs(t, err, ErrRepositoryNotFound)
	assert.Nil(t, sto)
}
