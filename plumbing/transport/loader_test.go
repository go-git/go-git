package transport

import (
	"net/url"
	"os"
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

func TestFilesystemLoader_LoadWithConfigDir(t *testing.T) {
	t.Parallel()
	// Create a temporary directory structure that mimics a non-bare repository
	// with a "config" directory in the working tree (which exists in go-git's own repo)
	tmpDir := t.TempDir()

	// Create repo directory
	repoDir := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.Mkdir(repoDir, 0o755))

	// Create .git directory with a config file
	gitDir := filepath.Join(repoDir, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644))

	// Create a "config" directory in the working tree (not in .git)
	configDir := filepath.Join(repoDir, "config")
	require.NoError(t, os.Mkdir(configDir, 0o755))

	// The loader should find .git/config, not the config directory
	loader := NewFilesystemLoader(osfs.New(tmpDir), false)
	u := &url.URL{Path: "repo"}

	st, err := loader.Load(u)
	require.NoError(t, err)
	require.NotNil(t, st)

	// Verify it loaded the correct config
	cfg, err := st.Config()
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

func TestFilesystemLoader_LoadWithGitfile(t *testing.T) {
	t.Parallel()
	// Test loading a repository where .git is a file (gitfile) pointing to the real git directory
	// This is common in worktrees and submodules
	tmpDir := t.TempDir()

	// Create the actual git directory
	realGitDir := filepath.Join(tmpDir, "real-git")
	require.NoError(t, os.MkdirAll(realGitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(realGitDir, "config"), []byte("[core]\n"), 0o644))

	// Create working tree with .git file pointing to real git directory (absolute path)
	workTree := filepath.Join(tmpDir, "worktree")
	require.NoError(t, os.MkdirAll(workTree, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, ".git"), []byte("gitdir: "+realGitDir+"\n"), 0o644))

	// The loader should follow the gitfile and load the real git directory
	// Use root filesystem since gitfile contains absolute path
	loader := NewFilesystemLoader(osfs.New(""), false)
	u := &url.URL{Path: filepath.ToSlash(workTree)}

	st, err := loader.Load(u)
	require.NoError(t, err)
	require.NotNil(t, st)

	// Verify it loaded the correct config
	cfg, err := st.Config()
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

func TestFilesystemLoader_LoadWithRelativeGitfile(t *testing.T) {
	t.Parallel()
	// Test loading a repository where .git file contains a relative path
	tmpDir := t.TempDir()

	// Create the actual git directory
	realGitDir := filepath.Join(tmpDir, ".git-real")
	require.NoError(t, os.MkdirAll(realGitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(realGitDir, "config"), []byte("[core]\n"), 0o644))

	// Create working tree with .git file pointing to relative git directory
	workTree := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(workTree, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, ".git"), []byte("gitdir: ../.git-real\n"), 0o644))

	// The loader should follow the relative gitfile path
	loader := NewFilesystemLoader(osfs.New(tmpDir), false)
	u := &url.URL{Path: "repo"}

	st, err := loader.Load(u)
	require.NoError(t, err)
	require.NotNil(t, st)

	// Verify it loaded the correct config
	cfg, err := st.Config()
	require.NoError(t, err)
	require.NotNil(t, cfg)
}
