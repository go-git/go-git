package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

// Build a small superproject + nested target on disk, where the
// superproject's .gitmodules pulls the nested repo over file://.
// Returns the superproject's worktree root and the Submodule handle.
func setupFileSubmodule(t *testing.T) (*Repository, *Submodule) {
	t.Helper()
	t.Setenv("GIT_ALLOW_PROTOCOL", "")
	_ = os.Unsetenv("GIT_ALLOW_PROTOCOL")
	t.Setenv("GIT_PROTOCOL_FROM_USER", "")
	_ = os.Unsetenv("GIT_PROTOCOL_FROM_USER")

	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	parentDir := filepath.Join(root, "parent")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	require.NoError(t, os.MkdirAll(parentDir, 0o755))

	// Build a small repository that the submodule will point at.
	tgt, err := PlainInit(targetDir, false)
	require.NoError(t, err)
	tgtWT, err := tgt.Worktree()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "README"), []byte("hello"), 0o644))
	_, err = tgtWT.Add("README")
	require.NoError(t, err)
	tgtHash, err := tgtWT.Commit("init", &CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example"},
	})
	require.NoError(t, err)
	require.NoError(t, tgt.Close())

	// Build a parent repository with a .gitmodules entry pointing at the
	// target over file://. Commit the gitlink at the recorded hash so
	// the submodule update has a concrete commit to fetch and check out.
	parent, err := PlainInit(parentDir, false)
	require.NoError(t, err)
	parentWT, err := parent.Worktree()
	require.NoError(t, err)

	// Normalize the path for the file:// URL: git config treats a
	// raw Windows backslash (`C:\Users\...`) as the start of an
	// escape sequence, so the parser rejects it before any submodule
	// machinery runs. Forward slashes work on every platform.
	urlPath := filepath.ToSlash(targetDir)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	gitmodules := "[submodule \"sub\"]\n\tpath = sub\n\turl = file://" + urlPath + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, ".gitmodules"), []byte(gitmodules), 0o644))

	// Stage the submodule as a gitlink at tgtHash. Add() on the
	// directory would try to crawl it as a worktree, so we mint the
	// index entry directly.
	idx, err := parent.Storer.Index()
	require.NoError(t, err)
	idx.Entries = append(idx.Entries, &index.Entry{
		Name: "sub",
		Hash: tgtHash,
		Mode: filemode.Submodule,
	})
	require.NoError(t, parent.Storer.SetIndex(idx))

	_, err = parentWT.Add(".gitmodules")
	require.NoError(t, err)
	_, err = parentWT.Commit("add submodule", &CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example"},
	})
	require.NoError(t, err)

	sm, err := parentWT.Submodule("sub")
	require.NoError(t, err)
	require.NoError(t, sm.Init())

	return parent, sm
}

func TestSubmoduleUpdate_FileSchemeDeniedByDefault(t *testing.T) { //nolint:paralleltest // mutates process env
	parent, sm := setupFileSubmodule(t)
	defer func() { _ = parent.Close() }()

	err := sm.UpdateContext(context.Background(), &SubmoduleUpdateOptions{})
	if !errors.Is(err, transport.ErrProtocolNotAllowed) {
		t.Fatalf("Update err = %v, want ErrProtocolNotAllowed", err)
	}
}

func TestSubmoduleUpdate_FileSchemeAllowedByProtocolConfig(t *testing.T) { //nolint:paralleltest // mutates process env
	parent, sm := setupFileSubmodule(t)
	defer func() { _ = parent.Close() }()

	subRepo, err := sm.Repository()
	require.NoError(t, err)
	defer func() { _ = subRepo.Close() }()

	cfg, err := subRepo.Config()
	require.NoError(t, err)
	cfg.Protocol.AllowByName = map[string]string{"file": config.ProtocolAlways}
	require.NoError(t, subRepo.Storer.SetConfig(cfg))

	require.NoError(t, sm.UpdateContext(context.Background(), &SubmoduleUpdateOptions{}))
}

// Canonical Git's submodule subprocess reads its own local config
// (plus global/system), never the parent's local config. A
// `protocol.file.allow=always` set on the superproject must not
// unlock the submodule fetch.
//
// Reference: https://github.com/git/git/blob/v2.54.0/git-submodule.sh#L29-L30
func TestSubmoduleUpdate_ParentPolicyDoesNotGovern(t *testing.T) { //nolint:paralleltest // mutates process env
	parent, sm := setupFileSubmodule(t)
	defer func() { _ = parent.Close() }()

	parentCfg, err := parent.Config()
	require.NoError(t, err)
	parentCfg.Protocol.AllowByName = map[string]string{"file": config.ProtocolAlways}
	require.NoError(t, parent.Storer.SetConfig(parentCfg))

	err = sm.UpdateContext(context.Background(), &SubmoduleUpdateOptions{})
	require.True(t, errors.Is(err, transport.ErrProtocolNotAllowed),
		"err=%v want ErrProtocolNotAllowed (parent config must not unblock submodule)", err)
}

func TestSubmoduleUpdate_FileSchemeAllowedByEnv(t *testing.T) {
	parent, sm := setupFileSubmodule(t)
	defer func() { _ = parent.Close() }()

	t.Setenv("GIT_ALLOW_PROTOCOL", "file")

	require.NoError(t, sm.UpdateContext(context.Background(), &SubmoduleUpdateOptions{}))
}

// Confirm the storage helper used above is sound; reading config after
// SetConfig must round-trip the new keys.
func TestProtocolPolicyConfigRoundTrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fs := osfs.New(tmp)
	st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	cfg, err := st.Config()
	require.NoError(t, err)
	cfg.Protocol.Allow = "user"
	cfg.Protocol.AllowByName = map[string]string{"file": "always"}
	require.NoError(t, st.SetConfig(cfg))

	got, err := st.Config()
	require.NoError(t, err)
	require.Equal(t, "user", got.Protocol.Allow)
	require.Equal(t, "always", got.Protocol.AllowByName["file"])
}

// errConfigStorer is a storer whose config cannot be read. Everything
// else is delegated to an in-memory storage so the value is usable
// wherever a storage.Storer is expected.
type errConfigStorer struct {
	storage.Storer
	err error
}

func (s *errConfigStorer) Config() (*config.Config, error) {
	return nil, s.err
}

func newErrConfigStorer(err error) *errConfigStorer {
	return &errConfigStorer{Storer: memory.NewStorage(), err: err}
}

// A config that cannot be read must abort the operation. Swallowing the
// error would drop the repository's protocol policy and let the request
// continue under the built-in defaults, which are more permissive than
// any policy a user would have written down.
func TestRemoteConfigReadErrorFailsOperation(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("config is unreadable")
	rc := &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"https://localhost/repo.git"},
		Fetch: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}

	for _, tc := range []struct {
		name string
		run  func(*Remote) error
	}{
		{"List", func(r *Remote) error {
			_, err := r.ListContext(context.Background(), &ListOptions{})
			return err
		}},
		{"Fetch", func(r *Remote) error {
			return r.FetchContext(context.Background(), &FetchOptions{})
		}},
		{"Push", func(r *Remote) error {
			return r.PushContext(context.Background(), &PushOptions{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := NewRemote(newErrConfigStorer(sentinel), rc)
			err := tc.run(r)
			require.ErrorIs(t, err, sentinel)
		})
	}
}

// The submodule's own config supplies its protocol policy, so a read
// failure must surface instead of leaving the fetch to run under the
// built-in defaults.
func TestSubmoduleClientOptionsConfigReadError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("config is unreadable")
	subRepo := &Repository{Storer: newErrConfigStorer(sentinel)}

	var s Submodule
	opts, err := s.submoduleClientOptions(subRepo, nil)
	require.ErrorIs(t, err, sentinel)
	require.Nil(t, opts)
}

// A submodule whose config cannot be read must not fall through to a
// fetch; UpdateContext has to report the failure.
func TestSubmoduleUpdate_ConfigReadErrorFailsUpdate(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("config is unreadable")
	subRepo := &Repository{Storer: newErrConfigStorer(sentinel)}

	var s Submodule
	err := s.fetchAndCheckout(
		context.Background(), subRepo, &SubmoduleUpdateOptions{}, plumbing.ZeroHash,
	)
	require.ErrorIs(t, err, sentinel)
}
