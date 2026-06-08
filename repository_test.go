package git

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	archivePkg "github.com/go-git/go-git/v6/internal/archive"
	"github.com/go-git/go-git/v6/internal/server"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/x/plugin"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

func TestInit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       func() []InitOption
		wantBare   bool
		wantBranch string
	}{
		{
			name:     "Bare",
			opts:     func() []InitOption { return []InitOption{} },
			wantBare: true,
		},
		{
			name: "With Worktree",
			opts: func() []InitOption {
				return []InitOption{WithWorkTree(memfs.New())}
			},
		},
		{
			name: "With Default Branch",
			opts: func() []InitOption {
				return []InitOption{
					WithWorkTree(memfs.New()),
					WithDefaultBranch("refs/head/foo"),
				}
			},
			wantBranch: "refs/head/foo",
		},
	}

	forEachFormat(t, func(t *testing.T, of formatcfg.ObjectFormat) {
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				opts := append(tc.opts(), WithObjectFormat(of))
				r, err := Init(memory.NewStorage(memory.WithObjectFormat(of)), opts...)
				require.NotNil(t, r)
				require.NoError(t, err)
				defer func() { _ = r.Close() }()

				cfg, err := r.Config()
				require.NoError(t, err)
				assert.Equal(t, tc.wantBare, cfg.Core.IsBare)
				assert.Equal(t, of, cfg.Extensions.ObjectFormat, "object format mismatch")

				if !tc.wantBare {
					h := createCommit(t, r)
					assert.Equal(t, of.HexSize(), len(h.String()))

					wantBranch := tc.wantBranch
					if wantBranch == "" {
						wantBranch = plumbing.Master.String()
					}

					ref, err := r.Head()
					require.NoError(t, err)
					require.Equal(t, wantBranch, ref.Name().String())
				}
			})
		}
	})
}

func TestPlainInitAndPlainOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       func() []InitOption
		wantBare   bool
		wantBranch string
	}{
		{
			name:     "Bare",
			opts:     func() []InitOption { return nil },
			wantBare: true,
		},
		{
			name: "With Worktree",
			opts: func() []InitOption {
				return []InitOption{WithWorkTree(memfs.New())}
			},
		},
		{
			name: "With Default Branch",
			opts: func() []InitOption {
				return []InitOption{
					WithWorkTree(memfs.New()),
					WithDefaultBranch("refs/head/foo"),
				}
			},
			wantBranch: "refs/head/foo",
		},
	}

	forEachFormat(t, func(t *testing.T, of formatcfg.ObjectFormat) {
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				opts := append(tc.opts(), WithObjectFormat(of))
				rdir := t.TempDir()

				r, err := PlainInit(rdir, tc.wantBare, opts...)
				require.NotNil(t, r)
				require.NoError(t, err)
				defer func() { _ = r.Close() }()

				cfg, err := r.Config()
				require.NoError(t, err)
				assert.Equal(t, tc.wantBare, cfg.Core.IsBare)

				if !tc.wantBare {
					h := createCommit(t, r)
					assert.Equal(t, of.HexSize(), len(h.String()))

					wantBranch := tc.wantBranch
					if wantBranch == "" {
						wantBranch = plumbing.Master.String()
					}

					ref, err := r.Head()
					require.NoError(t, err)
					require.Equal(t, wantBranch, ref.Name().String())
				}

				ro, err := PlainOpen(rdir)
				require.NotNil(t, ro)
				require.NoError(t, err)
				defer func() { _ = ro.Close() }()

				if !tc.wantBare {
					ref, err := ro.Head()
					require.NoError(t, err)
					assert.Equal(t, of.HexSize(), len(ref.Hash().String()))
				}
			})
		}
	})
}

type RepositorySuite struct {
	BaseSuite
}

func TestRepositorySuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(RepositorySuite))
}

func (s *RepositorySuite) TestInitWithInvalidDefaultBranch() {
	r, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()),
		WithDefaultBranch("foo"),
	)
	if r != nil {
		defer func() { _ = r.Close() }()
	}
	s.NotNil(err)
}

func (s *RepositorySuite) TestInitNonStandardDotGit() {
	dir := s.T().TempDir()
	fs := osfs.New(dir)
	dot, _ := fs.Chroot("storage")
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	wt, _ := fs.Chroot("worktree")
	r, err := Init(st, WithWorkTree(wt))
	s.NoError(err)
	s.NotNil(r)
	defer func() { _ = r.Close() }()

	f, err := fs.Open(fs.Join("worktree", ".git"))
	s.NoError(err)
	defer func() { _ = f.Close() }()

	all, err := io.ReadAll(f)
	s.NoError(err)
	s.Equal(string(all), fmt.Sprintf("gitdir: %s\n", filepath.Join("..", "storage")))

	cfg, err := r.Config()
	s.NoError(err)
	s.Equal(cfg.Core.Worktree, filepath.Join("..", "worktree"))
}

func (s *RepositorySuite) TestInitStandardDotGit() {
	dir := s.T().TempDir()
	fs := osfs.New(dir)
	dot, _ := fs.Chroot(".git")
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	r, err := Init(st, WithWorkTree(fs))
	s.NoError(err)
	s.NotNil(r)
	defer func() { _ = r.Close() }()

	l, err := fs.ReadDir(".git")
	s.NoError(err)
	s.True(len(l) > 0)

	cfg, err := r.Config()
	s.NoError(err)
	s.Equal("", cfg.Core.Worktree)
}

func (s *RepositorySuite) TestInitAlreadyExists() {
	st := memory.NewStorage()

	r, err := Init(st)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	r, err = Init(st)
	if r != nil {
		defer func() { _ = r.Close() }()
	}
	s.ErrorIs(err, ErrTargetDirNotEmpty)
	s.Nil(r)
}

func (s *RepositorySuite) TestOpen() {
	st := memory.NewStorage()

	r, err := Init(st, WithWorkTree(memfs.New()))
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	r, err = Open(st, memfs.New())
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()
}

func (s *RepositorySuite) TestOpenBare() {
	st := memory.NewStorage()

	r, err := Init(st)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	r, err = Open(st, nil)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()
}

func (s *RepositorySuite) TestOpenBareMissingWorktree() {
	st := memory.NewStorage()

	r, err := Init(st, WithWorkTree(memfs.New()))
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	r, err = Open(st, nil)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()
}

func (s *RepositorySuite) TestOpenNotExists() {
	r, err := Open(memory.NewStorage(), nil)
	s.ErrorIs(err, ErrRepositoryNotExists)
	s.Nil(r)
}

func (s *RepositorySuite) TestClone() {
	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)
	defer func() { _ = r.Close() }()

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)
}

func TestCloneAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag        string
		fixOF      string
		format     formatcfg.ObjectFormat
		refs       int
		plainClone bool
	}{
		{tag: ".git", fixOF: "sha256", format: formatcfg.SHA256, refs: 4},
		{tag: ".git", fixOF: "sha1", format: formatcfg.UnsetObjectFormat, refs: 11},
		{tag: ".git", fixOF: "sha256", format: formatcfg.SHA256, refs: 4, plainClone: true},
		{tag: ".git", fixOF: "sha1", format: formatcfg.UnsetObjectFormat, refs: 11, plainClone: true},
	}

	for _, tc := range tests {
		testName := fmt.Sprintf("%s/%s/plain=%t", tc.tag, tc.fixOF, tc.plainClone)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			f := fixtures.ByTag(tc.tag).ByObjectFormat(tc.fixOF).One()

			for _, srv := range server.All(server.Loader(t, f)) {
				endpoint, err := srv.Start()
				require.NoError(t, err)

				t.Cleanup(func() {
					require.NoError(t, srv.Close())
				})

				var r *Repository
				if tc.plainClone {
					r, err = PlainClone(t.TempDir(), &CloneOptions{URL: endpoint})
				} else {
					r, err = Clone(memory.NewStorage(), nil, &CloneOptions{
						URL: endpoint,
					})
				}
				require.NoError(t, err)
				require.NotNil(t, r, "repository must not be nil")
				defer func() { _ = r.Close() }()

				remotes, err := r.Remotes()
				require.NoError(t, err)
				assert.Len(t, remotes, 1)

				iter, err := r.References()
				require.NoError(t, err)

				refs := 0
				iter.ForEach(func(_ *plumbing.Reference) error {
					refs++
					return nil
				})
				assert.Equal(t, tc.refs, refs)

				cfg, err := r.Config()
				require.NoError(t, err)

				assert.Equal(t, tc.format, cfg.Extensions.ObjectFormat)

				ref, err := r.Head()
				require.NoError(t, err, "failed to get repository HEAD ref")

				c, err := r.CommitObject(ref.Hash())
				require.NoError(t, err, "failed to get commit object")
				assert.NotNil(t, c)
			}
		})
	}
}

func TestFetchMustNotUpdateObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		clientFormat formatcfg.ObjectFormat
		serverTag    string
		fixOF        string
		wantErr      bool
	}{
		{
			name:         "unset client format cannot fetch sha256",
			clientFormat: formatcfg.UnsetObjectFormat,
			serverTag:    ".git",
			fixOF:        "sha256",
			wantErr:      true,
		},
		{
			name:         "sha1 client cannot fetch sha256",
			clientFormat: formatcfg.SHA1,
			serverTag:    ".git",
			fixOF:        "sha256",
			wantErr:      true,
		},
		{
			name:         "sha256 client cannot fetch sha1",
			clientFormat: formatcfg.SHA256,
			serverTag:    ".git",
			fixOF:        "sha1",
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := fixtures.ByTag(tc.serverTag).ByObjectFormat(tc.fixOF).One()
			require.NotNil(t, f, "fixture not found for tag %s", tc.serverTag)

			for _, srv := range server.All(server.Loader(t, f)) {
				endpoint, err := srv.Start()
				require.NoError(t, err)

				t.Cleanup(func() {
					require.NoError(t, srv.Close())
				})

				var st *memory.Storage
				if tc.clientFormat == formatcfg.UnsetObjectFormat {
					st = memory.NewStorage()
				} else {
					st = memory.NewStorage(memory.WithObjectFormat(tc.clientFormat))
				}

				r, err := Init(st)
				require.NoError(t, err)
				defer func() { _ = r.Close() }()

				_, err = r.CreateRemote(&config.RemoteConfig{
					Name: DefaultRemoteName,
					URLs: []string{endpoint},
				})
				require.NoError(t, err)

				err = r.Fetch(&FetchOptions{})
				if tc.wantErr {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "mismatched algorithms")
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}

// TestFetchByHashThenResolveRevision is a regression test for two bugs that
// affected fetching a specific commit by hash into an existing repository
// using the force-refspec "+<hash>:<hash>".
//
// Regression 1 (negotiate.go): shallow boundaries were not persisted after a
// clone, so subsequent fetches sent no "shallow" lines to the server. The
// server then inferred the client already owned the wanted commit (a deep
// ancestor of the shallow tip) and returned an empty packfile, leaving the
// object absent from the store.
//
// Regression 2 (remote.go): when the dst of a refspec is a bare SHA, the
// code created a branch named after the hash (refs/heads/<sha>) pointing to
// the zero hash. ResolveRevision resolved via that spurious ref and returned
// the zero hash instead of the real commit hash, breaking all downstream
// callers such as Worktree.Checkout.
//
// Note: this test uses the live GitHub URL rather than a local fixture server
// because the fixture HTTP server does not implement depth filtering in
// upload-pack. Without depth support the server sends all objects on a
// depth=1 clone, making commitSHA present from the start and preventing
// regression 1 from being reproduced.
func TestFetchByHashThenResolveRevision(t *testing.T) {
	t.Parallel()

	// git-fixtures/basic.git commit graph (abbreviated):
	//
	//   * 6ecf0ef  vendor stuff          <- refs/heads/master (HEAD)
	//   | * e8d3ffa some code in branch  <- refs/heads/branch
	//   |/
	//   * 918c48b  some code
	//   ...several more commits...
	//   * 35e8510  binary file            <- commitSHA (deep ancestor of both)
	//   * b029517  Initial commit
	//
	// A depth=1 shallow clone of master fetches only 6ecf0ef2. The target
	// commit (35e85108) is a deep historical ancestor, absent because it is
	// pruned by the shallow depth limit — not because it is on a different branch.
	const (
		repoURL   = "https://github.com/git-fixtures/basic.git"
		commitSHA = "35e85108805c84807bc66a02d91535e1e24b38b9"
	)

	tmp := t.TempDir()

	// Step 1: clone the default branch at depth=1.
	// commitSHA is a deep ancestor excluded by the shallow depth limit.
	r, err := PlainClone(tmp, &CloneOptions{
		URL:   repoURL,
		Depth: 1,
		Tags:  NoTags,
	})
	require.NoError(t, err, "clone should succeed")
	defer func() { _ = r.Close() }()

	// Confirm the target commit is not yet available.
	_, err = r.CommitObject(plumbing.NewHash(commitSHA))
	require.Error(t, err, "commit should NOT be present in a shallow clone of master")

	// Step 2: fetch the specific commit by hash using "+<hash>:<hash>".
	// This is the standard refspec for fetching an arbitrary commit that is not
	// the tip of a branch or tag.
	refSpec := config.RefSpec("+" + commitSHA + ":" + commitSHA)
	err = r.Fetch(&FetchOptions{
		Depth:    1,
		Force:    true,
		RefSpecs: []config.RefSpec{refSpec},
		Tags:     NoTags,
	})
	require.NoError(t, err, "fetch should succeed")

	// Step 3: the commit object MUST now be present in the object store.
	_, err = r.CommitObject(plumbing.NewHash(commitSHA))
	assert.NoError(t, err,
		"commit object must be present in the object store after a successful Fetch with '+<hash>:<hash>'",
	)

	// Step 4: regression 2 — no spurious refs/heads/<sha> must be created.
	// Before the fix, updateLocalReferenceStorage created refs/heads/<sha>
	// pointing to the zero hash for any bare-hash dst refspec.
	_, refErr := r.Storer.Reference(plumbing.NewBranchReferenceName(commitSHA))
	assert.Error(t, refErr,
		"fetching by hash must NOT create a branch named after the hash")

	// Step 5: the commit must also be resolvable as a revision.
	// Before the fix, the spurious branch caused ResolveRevision to return
	// the zero hash.
	hash, err := r.ResolveRevision(plumbing.Revision(commitSHA))
	assert.NoError(t, err,
		"ResolveRevision with a full SHA must succeed when the commit object is present",
	)
	if hash != nil {
		assert.Equal(t, commitSHA, hash.String())
	}

	// Step 6: downstream effect — Worktree.Checkout must also succeed.
	w, err := r.Worktree()
	require.NoError(t, err)
	err = w.Checkout(&CheckoutOptions{
		Hash:  plumbing.NewHash(commitSHA),
		Force: true,
	})
	assert.NoError(t, err, "Worktree.Checkout by hash should succeed after fetching the commit")
}

// TestPlainCloneContext_FailedCloneRemovesCreatedDirectory is a regression test
// for a v6 behaviour difference vs v5 and the reference git implementation.
//
// When PlainCloneContext creates the destination directory (i.e. it did not
// exist before the call) and the clone subsequently fails, it must remove the
// directory it created — just as `git clone` does. Without this cleanup a
// caller that retries with different credentials (e.g. iterating over auth
// methods) gets "destination path already exists" on the second attempt.
func TestPlainCloneContext_FailedCloneRemovesCreatedDirectory(t *testing.T) {
	t.Parallel()

	// dest does not exist yet; PlainCloneContext must create and then remove it.
	dest := filepath.Join(t.TempDir(), "repo")

	_, err := PlainCloneContext(context.Background(), dest, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	require.Error(t, err)

	_, statErr := os.Stat(dest)
	assert.True(t, os.IsNotExist(statErr),
		"PlainCloneContext must remove the directory it created when the clone fails")
}

// TestPlainCloneContext_FailedClonePreservesPreexistingEmptyDirectory verifies
// that a directory which already existed (but was empty) before the call is
// preserved after a failed clone — matching `git clone` behaviour.
func TestPlainCloneContext_FailedClonePreservesPreexistingEmptyDirectory(t *testing.T) {
	t.Parallel()

	// dest exists and is empty before the clone attempt.
	dest := t.TempDir()

	_, err := PlainCloneContext(context.Background(), dest, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	require.Error(t, err)

	// The directory itself must still be there …
	_, statErr := os.Stat(dest)
	assert.NoError(t, statErr, "PlainCloneContext must not remove a pre-existing directory")

	// … and must be empty (any .git content added by PlainInit must be removed).
	entries, _ := os.ReadDir(dest)
	assert.Empty(t, entries,
		"PlainCloneContext must remove any content it added to a pre-existing empty directory")
}

// TestPlainCloneContext_EmptyRemoteReturnsError verifies that cloning an
// empty remote repository returns ErrEmptyRemoteRepository by default.
func TestPlainCloneContext_EmptyRemoteReturnsError(t *testing.T) {
	t.Parallel()

	remote := filepath.Join(t.TempDir(), "remote.git")
	remoteRepo, err := PlainInit(remote, true)
	require.NoError(t, err)
	_ = remoteRepo.Close()

	dest := filepath.Join(t.TempDir(), "clone")
	_, err = PlainCloneContext(context.Background(), dest, &CloneOptions{
		URL: remote,
	})
	require.ErrorIs(t, err, transport.ErrEmptyRemoteRepository)
}

// TestPlainCloneContext_EmptyRemoteDoesNotCleanup verifies that cloning an
// empty remote repository with AllowEmptyRepo does not remove the directory.
func TestPlainCloneContext_EmptyRemoteDoesNotCleanup(t *testing.T) {
	t.Parallel()

	// Create a bare empty repository to use as the remote.
	remote := filepath.Join(t.TempDir(), "remote.git")
	remoteRepo, err := PlainInit(remote, true)
	require.NoError(t, err)
	defer func() { _ = remoteRepo.Close() }()

	dest := filepath.Join(t.TempDir(), "clone")
	r, err := PlainCloneContext(context.Background(), dest, &CloneOptions{
		URL:            remote,
		AllowEmptyRepo: true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	defer func() { _ = r.Close() }()

	// The cloned repo should have the "origin" remote configured.
	remotes, err := r.Remotes()
	require.NoError(t, err)
	require.Len(t, remotes, 1)
	assert.Equal(t, "origin", remotes[0].Config().Name)

	// The .git directory must still exist — the repo was initialized successfully.
	_, statErr := os.Stat(filepath.Join(dest, GitDirName))
	assert.NoError(t, statErr,
		"PlainCloneContext must not remove .git when cloning an empty remote")

	// HEAD must be a valid symbolic reference (not .invalid), because
	// AllowEmptyRepo skips withPartialInit and fully initialises the repo.
	head, err := r.Reference(plumbing.HEAD, false)
	require.NoError(t, err)
	assert.Equal(t, plumbing.SymbolicReference, head.Type())
	assert.NotEqual(t, plumbing.Invalid, head.Target(),
		"HEAD must not point to .invalid when AllowEmptyRepo is set")

	// The repository should be re-openable — a fully initialised repo has
	// its config persisted (unlike a partialInit repo).
	reopened, err := PlainOpen(dest)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()
	reopenedRemotes, err := reopened.Remotes()
	require.NoError(t, err)
	require.Len(t, reopenedRemotes, 1)
	assert.Equal(t, "origin", reopenedRemotes[0].Config().Name)
}

// TestPlainCloneContext_DetachedHeadSource is a regression test for a bug
// where PlainCloneContext returns plumbing.ErrReferenceNotFound when the
// source repository has a detached HEAD.
//
// A detached HEAD is the normal state of a repository after
// Worktree.Checkout is called with CheckoutOptions{Hash: someHash}.
// Cloning FROM such a repo (e.g. when using a local filesystem directory
// as a "remote") must succeed, just as `git clone` does.
func TestPlainCloneContext_DetachedHeadSource(t *testing.T) {
	t.Parallel()

	// ── Build the source repo using PlainInit + Worktree.Commit ──────────
	// Self-contained: no network access required.
	srcDir := t.TempDir()
	src, err := PlainInit(srcDir, false)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	wt, err := src.Worktree()
	require.NoError(t, err)

	// Write a file and create an initial commit so HEAD resolves to a real hash.
	err = os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("hello"), 0o644)
	require.NoError(t, err)
	_, err = wt.Add("README.md")
	require.NoError(t, err)
	commitHash, err := wt.Commit("initial commit", &CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@t.com"},
	})
	require.NoError(t, err)

	// Verify HEAD is a symbolic reference before detaching.
	rawHead, err := src.Storer.Reference(plumbing.HEAD)
	require.NoError(t, err)
	require.Equal(t, plumbing.SymbolicReference, rawHead.Type(),
		"HEAD should be a symbolic ref after commit")

	// Detach HEAD by checking out by hash.
	err = wt.Checkout(&CheckoutOptions{Hash: commitHash, Force: true})
	require.NoError(t, err)

	detachedHead, err := src.Storer.Reference(plumbing.HEAD)
	require.NoError(t, err)
	require.Equal(t, plumbing.HashReference, detachedHead.Type(),
		"HEAD must be a hash-reference (detached) after Checkout{Hash:...}")

	// ── Clone from the detached-HEAD source ───────────────────────────────
	// The branch ref (refs/heads/master) still exists in the source object
	// store; only HEAD is detached. PlainCloneContext must succeed — just as
	// `git clone` does — by advertising the available refs rather than
	// requiring HEAD to be symbolic.
	dstDir := filepath.Join(t.TempDir(), "dst")
	dst, err := PlainCloneContext(context.Background(), dstDir, &CloneOptions{
		URL: srcDir,
	})
	assert.NoError(t, err,
		"PlainCloneContext must succeed when the source repo has a detached HEAD")

	// ── Verify the cloned repo behaves like `git clone` ──────────────────────
	// git clone creates a symbolic HEAD (→ refs/heads/<branch>) in the clone
	// even when the source has a detached HEAD; the resolved commit must match.
	require.NotNil(t, dst, "cloned repository must not be nil")
	defer func() { _ = dst.Close() }()

	rawClonedHead, err := dst.Storer.Reference(plumbing.HEAD)
	require.NoError(t, err)
	assert.Equal(t, plumbing.SymbolicReference, rawClonedHead.Type(),
		"cloned repo HEAD must be a symbolic ref (as git clone produces), not detached")

	resolvedHead, err := dst.Head()
	require.NoError(t, err)
	assert.Equal(t, commitHash, resolvedHead.Hash(),
		"cloned repo HEAD must resolve to the same commit as the source")
}

// sha1OnlyStorage wraps a storage.Storer to hide the ExtensionChecker
// implementation, simulating a storage backend that does not implement
// that interface.
type sha1OnlyStorage struct {
	storage.Storer
}

func TestFailSafeUnsupportedStorage(t *testing.T) {
	t.Parallel()

	t.Run("clone", func(t *testing.T) {
		t.Parallel()

		f := fixtures.ByTag(".git").ByObjectFormat("sha256").One()
		require.NotNil(t, f, "fixture not found")

		for _, srv := range server.All(server.Loader(t, f)) {
			endpoint, err := srv.Start()
			require.NoError(t, err)

			t.Cleanup(func() {
				require.NoError(t, srv.Close())
			})

			st := &sha1OnlyStorage{memory.NewStorage()}
			_, okGetter := storage.Storer(st).(xstorage.ExtensionChecker)
			assert.False(t, okGetter, "sha1OnlyStorage must not implement ExtensionChecker")

			r, err := Clone(st, nil, &CloneOptions{URL: endpoint})
			if r != nil {
				defer func() { _ = r.Close() }()
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "mismatched algorithms")
		}
	})

	t.Run("open", func(t *testing.T) {
		t.Parallel()

		f := fixtures.ByTag(".git").ByObjectFormat("sha256").One()
		require.NotNil(t, f, "fixture not found")

		dotgit, dotgitErr := f.DotGit(fixtures.WithMemFS())
		require.NoError(t, dotgitErr)
		st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
		defer func() { _ = st.Close() }()

		wrapped := &sha1OnlyStorage{st}
		_, okGetter := storage.Storer(wrapped).(xstorage.ExtensionChecker)
		assert.False(t, okGetter, "sha1OnlyStorage must not implement ExtensionChecker")

		r, err := Open(wrapped, nil)
		assert.Error(t, err)
		assert.Nil(t, r)
	})
}

func (s *RepositorySuite) TestCloneContext() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r, err := CloneContext(ctx, memory.NewStorage(), nil, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NotNil(r)
	_ = r.Close()
	s.ErrorIs(err, context.Canceled)
}

func (s *RepositorySuite) TestCloneMirror() {
	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{
		URL:    fixtures.Basic().One().URL,
		Mirror: true,
	})

	s.NoError(err)
	defer func() { _ = r.Close() }()

	refs, err := r.References()
	var count int
	refs.ForEach(func(r *plumbing.Reference) error { s.T().Log(r); count++; return nil })
	s.NoError(err)
	// 6 refs total from github.com/git-fixtures/basic.git:
	//  - HEAD
	//  - refs/heads/master
	//  - refs/heads/branch
	//  - refs/pull/1/head
	//  - refs/pull/2/head
	//  - refs/pull/2/merge
	s.Equal(6, count)

	cfg, err := r.Config()
	s.NoError(err)

	s.True(cfg.Core.IsBare)
	s.Nil(cfg.Remotes[DefaultRemoteName].Validate())
	s.True(cfg.Remotes[DefaultRemoteName].Mirror)
}

func (s *RepositorySuite) TestCloneWithTags() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{URL: url, Tags: NoTags})
	s.NoError(err)
	defer func() { _ = r.Close() }()

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)

	i, err := r.References()
	s.NoError(err)

	var count int
	i.ForEach(func(*plumbing.Reference) error { count++; return nil })

	s.Equal(3, count)
}

func (s *RepositorySuite) TestCloneSparse() {
	fs := memfs.New()
	r, err := Clone(memory.NewStorage(), fs, &CloneOptions{
		URL:        s.GetBasicLocalRepositoryURL(),
		NoCheckout: true,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)

	sparseCheckoutDirectories := []string{"go", "json", "php"}
	s.NoError(w.Checkout(&CheckoutOptions{
		Branch:                    "refs/heads/master",
		SparseCheckoutDirectories: sparseCheckoutDirectories,
	}))

	fis, err := fs.ReadDir(".")
	s.NoError(err)
	for _, fi := range fis {
		s.True(fi.IsDir())
		var oneOfSparseCheckoutDirs bool

		for _, sparseCheckoutDirectory := range sparseCheckoutDirectories {
			if strings.HasPrefix(fi.Name(), sparseCheckoutDirectory) {
				oneOfSparseCheckoutDirs = true
			}
		}
		s.True(oneOfSparseCheckoutDirs)
	}
}

func (s *RepositorySuite) TestCreateRemoteAndRemote() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	remote, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{"http://foo/foo.git"},
	})

	s.NoError(err)
	s.Equal("foo", remote.Config().Name)

	alt, err := r.Remote("foo")
	s.NoError(err)
	s.NotSame(remote, alt)
	s.Equal("foo", alt.Config().Name)
}

func (s *RepositorySuite) TestCreateRemoteInvalid() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	remote, err := r.CreateRemote(&config.RemoteConfig{})

	s.ErrorIs(err, config.ErrRemoteConfigEmptyName)
	s.Nil(remote)
}

func (s *RepositorySuite) TestCreateRemoteAnonymous() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: "anonymous",
		URLs: []string{"http://foo/foo.git"},
	})

	s.NoError(err)
	s.Equal("anonymous", remote.Config().Name)
}

func (s *RepositorySuite) TestCreateRemoteAnonymousInvalidName() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: "not_anonymous",
		URLs: []string{"http://foo/foo.git"},
	})

	s.ErrorIs(err, ErrAnonymousRemoteName)
	s.Nil(remote)
}

func (s *RepositorySuite) TestCreateRemoteAnonymousInvalid() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{})

	s.ErrorIs(err, config.ErrRemoteConfigEmptyName)
	s.Nil(remote)
}

func (s *RepositorySuite) TestDeleteRemote() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{"http://foo/foo.git"},
	})

	s.NoError(err)

	err = r.DeleteRemote("foo")
	s.NoError(err)

	alt, err := r.Remote("foo")
	s.ErrorIs(err, ErrRemoteNotFound)
	s.Nil(alt)
}

func (s *RepositorySuite) TestEmptyCreateBranch() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.CreateBranch(&config.Branch{})

	s.NotNil(err)
}

func (s *RepositorySuite) TestInvalidCreateBranch() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.CreateBranch(&config.Branch{
		Name: "-foo",
	})

	s.NotNil(err)
}

func (s *RepositorySuite) TestCreateBranchAndBranch() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err := r.CreateBranch(testBranch)

	s.NoError(err)
	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	branch := cfg.Branches["foo"]
	s.Equal(testBranch.Name, branch.Name)
	s.Equal(testBranch.Remote, branch.Remote)
	s.Equal(testBranch.Merge, branch.Merge)

	branch, err = r.Branch("foo")
	s.NoError(err)
	s.Equal(testBranch.Name, branch.Name)
	s.Equal(testBranch.Remote, branch.Remote)
	s.Equal(testBranch.Merge, branch.Merge)
}

func (s *RepositorySuite) TestMergeFF() {
	r, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	s.NoError(err)
	s.NotNil(r)
	defer func() { _ = r.Close() }()

	createCommit(s.T(), r)
	createCommit(s.T(), r)
	createCommit(s.T(), r)
	lastCommit := createCommit(s.T(), r)

	wt, err := r.Worktree()
	s.NoError(err)

	targetBranch := plumbing.NewBranchReferenceName("foo")
	err = wt.Checkout(&CheckoutOptions{
		Hash:   lastCommit,
		Create: true,
		Branch: targetBranch,
	})
	s.NoError(err)

	createCommit(s.T(), r)
	fooHash := createCommit(s.T(), r)

	// Checkout the master branch so that we can try to merge foo into it.
	err = wt.Checkout(&CheckoutOptions{
		Branch: plumbing.Master,
	})
	s.NoError(err)

	head, err := r.Head()
	s.NoError(err)
	s.Equal(lastCommit, head.Hash())

	targetRef := plumbing.NewHashReference(targetBranch, fooHash)
	s.NotNil(targetRef)

	err = r.Merge(*targetRef, MergeOptions{
		Strategy: FastForwardMerge,
	})
	s.NoError(err)

	head, err = r.Head()
	s.NoError(err)
	s.Equal(fooHash, head.Hash())
}

func (s *RepositorySuite) TestMergeFF_Invalid() {
	r, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	s.NoError(err)
	s.NotNil(r)
	defer func() { _ = r.Close() }()

	// Keep track of the first commit, which will be the
	// reference to create the target branch so that we
	// can simulate a non-ff merge.
	firstCommit := createCommit(s.T(), r)
	createCommit(s.T(), r)
	createCommit(s.T(), r)
	lastCommit := createCommit(s.T(), r)

	wt, err := r.Worktree()
	s.NoError(err)

	targetBranch := plumbing.NewBranchReferenceName("foo")
	err = wt.Checkout(&CheckoutOptions{
		Hash:   firstCommit,
		Create: true,
		Branch: targetBranch,
	})

	s.NoError(err)

	createCommit(s.T(), r)
	h := createCommit(s.T(), r)

	// Checkout the master branch so that we can try to merge foo into it.
	err = wt.Checkout(&CheckoutOptions{
		Branch: plumbing.Master,
	})
	s.NoError(err)

	head, err := r.Head()
	s.NoError(err)
	s.Equal(lastCommit, head.Hash())

	targetRef := plumbing.NewHashReference(targetBranch, h)
	s.NotNil(targetRef)

	err = r.Merge(*targetRef, MergeOptions{
		Strategy: MergeStrategy(10),
	})
	s.ErrorIs(err, ErrUnsupportedMergeStrategy)

	// Failed merge operations must not change HEAD.
	head, err = r.Head()
	s.NoError(err)
	s.Equal(lastCommit, head.Hash())

	err = r.Merge(*targetRef, MergeOptions{})
	s.ErrorIs(err, ErrFastForwardMergeNotPossible)

	head, err = r.Head()
	s.NoError(err)
	s.Equal(lastCommit, head.Hash())
}

func (s *RepositorySuite) TestCreateBranchUnmarshal() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	expected := []byte(`[core]
	bare = true
	filemode = true
[remote "foo"]
	url = http://foo/foo.git
	fetch = +refs/heads/*:refs/remotes/foo/*
[branch "foo"]
	remote = origin
	merge = refs/heads/foo
[branch "master"]
	remote = origin
	merge = refs/heads/master
`)

	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{"http://foo/foo.git"},
	})
	s.NoError(err)
	testBranch1 := &config.Branch{
		Name:   "master",
		Remote: "origin",
		Merge:  "refs/heads/master",
	}
	testBranch2 := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err = r.CreateBranch(testBranch1)
	s.NoError(err)
	err = r.CreateBranch(testBranch2)
	s.NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	marshaled, err := cfg.Marshal()
	s.NoError(err)
	s.Equal(string(expected), string(marshaled))
}

func (s *RepositorySuite) TestBranchInvalid() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	branch, err := r.Branch("foo")

	s.NotNil(err)
	s.Nil(branch)
}

func (s *RepositorySuite) TestCreateBranchInvalid() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.CreateBranch(&config.Branch{})

	s.NotNil(err)

	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err = r.CreateBranch(testBranch)
	s.NoError(err)
	err = r.CreateBranch(testBranch)
	s.NotNil(err)
}

func (s *RepositorySuite) TestDeleteBranch() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err := r.CreateBranch(testBranch)

	s.NoError(err)

	err = r.DeleteBranch("foo")
	s.NoError(err)

	b, err := r.Branch("foo")
	s.ErrorIs(err, ErrBranchNotFound)
	s.Nil(b)

	err = r.DeleteBranch("foo")
	s.ErrorIs(err, ErrBranchNotFound)
}

func (s *RepositorySuite) TestDeleteBranchFullRefName() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err := r.CreateBranch(testBranch)
	s.NoError(err)

	err = r.DeleteBranch("refs/heads/foo")
	s.NoError(err)

	b, err := r.Branch("foo")
	s.ErrorIs(err, ErrBranchNotFound)
	s.Nil(b)
}

func (s *RepositorySuite) TestPlainInitAlreadyExists() {
	dir := s.T().TempDir()
	r, err := PlainInit(dir, true)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	r, err = PlainInit(dir, true)
	s.ErrorIs(err, ErrTargetDirNotEmpty)
	s.Nil(r)
}

func (s *RepositorySuite) TestPlainOpenTildePath() {
	dir, clean := s.TemporalHomeDir()
	defer clean()

	r, err := PlainInit(dir, false)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	currentUser, err := user.Current()
	s.NoError(err)
	// remove domain for windows
	username := currentUser.Username[strings.Index(currentUser.Username, "\\")+1:]

	homes := []string{"~/", "~" + username + "/"}
	for _, home := range homes {
		path := strings.Replace(dir, strings.Split(dir, ".tmp")[0], home, 1)

		r, err = PlainOpen(path)
		s.NoError(err)
		s.NotNil(r)
		_ = r.Close()
	}
}

func (s *RepositorySuite) testPlainOpenGitFile(f func(string, string) string) {
	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "plain-open")
	s.NoError(err)

	r, err := PlainInit(fs.Join(fs.Root(), dir), true)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	altDir, err := util.TempDir(fs, "", "plain-open")
	s.NoError(err)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"),
		[]byte(f(fs.Join(fs.Root(), dir), fs.Join(fs.Root(), altDir))),
		0o644,
	)

	s.NoError(err)

	r, err = PlainOpen(fs.Join(fs.Root(), altDir))
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()
}

func (s *RepositorySuite) TestPlainOpenBareAbsoluteGitDirFile() {
	s.testPlainOpenGitFile(func(dir, _ string) string {
		return fmt.Sprintf("gitdir: %s\n", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareAbsoluteGitDirFileNoEOL() {
	s.testPlainOpenGitFile(func(dir, _ string) string {
		return fmt.Sprintf("gitdir: %s", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFile() {
	s.testPlainOpenGitFile(func(dir, altDir string) string {
		dir, err := filepath.Rel(altDir, dir)
		s.NoError(err)
		return fmt.Sprintf("gitdir: %s\n", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileNoEOL() {
	s.testPlainOpenGitFile(func(dir, altDir string) string {
		dir, err := filepath.Rel(altDir, dir)
		s.NoError(err)
		return fmt.Sprintf("gitdir: %s\n", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileTrailingGarbage() {
	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	r, err := PlainInit(dir, true)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	altDir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"),
		fmt.Appendf(nil, "gitdir: %s\nTRAILING", fs.Join(fs.Root(), altDir)),
		0o644,
	)
	s.NoError(err)

	r2, err := PlainOpen(altDir)
	s.ErrorIs(err, ErrRepositoryNotExists)
	s.Nil(r2)
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileBadPrefix() {
	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	r, err := PlainInit(fs.Join(fs.Root(), dir), true)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	altDir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"),
		fmt.Appendf(nil, "xgitdir: %s\n", fs.Join(fs.Root(), dir)),
		0o644)

	s.NoError(err)

	r, err = PlainOpen(fs.Join(fs.Root(), altDir))
	s.ErrorContains(err, "gitdir")
	s.Nil(r)
}

func (s *RepositorySuite) TestPlainOpenNotExists() {
	r, err := PlainOpen("/not-exists/")
	s.ErrorIs(err, ErrRepositoryNotExists)
	s.Nil(r)
}

func (s *RepositorySuite) TestPlainOpenDetectDotGit() {
	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	subdir := filepath.Join(dir, "a", "b")
	err = fs.MkdirAll(subdir, 0o755)
	s.NoError(err)

	file := fs.Join(subdir, "file.txt")
	f, err := fs.Create(file)
	s.NoError(err)
	f.Close()

	r, err := PlainInit(fs.Join(fs.Root(), dir), false)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	opt := &PlainOpenOptions{DetectDotGit: true}
	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), subdir), opt)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), file), opt)
	s.NoError(err)
	s.NotNil(r)
	_ = r.Close()

	optnodetect := &PlainOpenOptions{DetectDotGit: false}
	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), file), optnodetect)
	s.NotNil(err)
	s.Nil(r)
}

func (s *RepositorySuite) TestPlainOpenNotExistsDetectDotGit() {
	dir := s.T().TempDir()
	opt := &PlainOpenOptions{DetectDotGit: true}
	r, err := PlainOpenWithOptions(dir, opt)
	s.ErrorIs(err, ErrRepositoryNotExists)
	s.Nil(r)
}

func (s *RepositorySuite) TestPlainClone() {
	dir := "rel-dir"
	err := os.Mkdir(dir, 0o755)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		os.RemoveAll(dir)
	})

	r, err := PlainClone(dir, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)
	defer func() { _ = r.Close() }()

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)
	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	s.Equal("master", cfg.Branches["master"].Name)
}

func (s *RepositorySuite) TestPlainCloneBareAndShared() {
	dir := s.T().TempDir()
	remote := s.GetBasicLocalRepositoryURL()

	r, err := PlainClone(dir, &CloneOptions{
		URL:    remote,
		Shared: true,
		Bare:   true,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

	altpath := path.Join(dir, "objects", "info", "alternates")
	_, err = os.Stat(altpath)
	s.NoError(err)

	data, err := os.ReadFile(altpath)
	s.NoError(err)

	line := path.Join(remote, GitDirName, "objects") + "\n"
	s.Equal(line, string(data))

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	s.Equal("master", cfg.Branches["master"].Name)
}

func (s *RepositorySuite) TestPlainCloneShared() {
	dir := s.T().TempDir()
	remote := s.GetBasicLocalRepositoryURL()

	r, err := PlainClone(dir, &CloneOptions{
		URL:    remote,
		Shared: true,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

	altpath := path.Join(dir, GitDirName, "objects", "info", "alternates")
	_, err = os.Stat(altpath)
	s.NoError(err)

	data, err := os.ReadFile(altpath)
	s.NoError(err)

	line := path.Join(remote, GitDirName, "objects") + "\n"
	s.Equal(line, string(data))

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	s.Equal("master", cfg.Branches["master"].Name)
}

func (s *RepositorySuite) TestPlainCloneSharedHttpShouldReturnError() {
	dir := s.T().TempDir()
	remote := "http://somerepo"

	_, err := PlainClone(dir, &CloneOptions{
		URL:    remote,
		Shared: true,
	})
	s.ErrorIs(err, ErrAlternatePathNotSupported)
}

func (s *RepositorySuite) TestPlainCloneSharedHttpsShouldReturnError() {
	dir := s.T().TempDir()
	remote := "https://somerepo"

	_, err := PlainClone(dir, &CloneOptions{
		URL:    remote,
		Shared: true,
	})
	s.ErrorIs(err, ErrAlternatePathNotSupported)
}

func (s *RepositorySuite) TestPlainCloneSharedSSHShouldReturnError() {
	dir := s.T().TempDir()
	remote := "ssh://somerepo"

	_, err := PlainClone(dir, &CloneOptions{
		URL:    remote,
		Shared: true,
	})
	s.ErrorIs(err, ErrAlternatePathNotSupported)
}

func (s *RepositorySuite) TestPlainCloneWithRemoteName() {
	dir := s.T().TempDir()
	r, err := PlainClone(dir, &CloneOptions{
		URL:        s.GetBasicLocalRepositoryURL(),
		RemoteName: "test",
	})

	s.NoError(err)
	defer func() { _ = r.Close() }()

	remote, err := r.Remote("test")
	s.NoError(err)
	s.NotNil(remote)
}

func (s *RepositorySuite) TestPlainCloneOverExistingGitDirectory() {
	dir := s.T().TempDir()
	r, err := PlainInit(dir, false)
	s.NotNil(r)
	s.NoError(err)
	_ = r.Close()

	r, err = PlainClone(dir, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.Nil(r)
	s.ErrorIs(err, ErrTargetDirNotEmpty)
}

func (s *RepositorySuite) TestPlainCloneContextCancel() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := s.T().TempDir()
	r, err := PlainCloneContext(ctx, dir, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NotNil(r)
	s.ErrorIs(err, context.Canceled)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithExistentDir() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	r, err := PlainCloneContext(ctx, dir, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	s.NotNil(r)
	s.ErrorIs(err, transport.ErrRepositoryNotFound)

	_, err = fs.Stat(dir)
	s.False(os.IsNotExist(err))

	names, err := fs.ReadDir(dir)
	s.NoError(err)
	s.Len(names, 0)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithNonExistentDir() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fs := s.TemporalFilesystem()

	tmpDir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	repoDir := filepath.Join(tmpDir, "repoDir")

	r, err := PlainCloneContext(ctx, repoDir, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	s.NotNil(r)
	s.ErrorIs(err, transport.ErrRepositoryNotFound)

	_, err = fs.Stat(repoDir)
	s.True(os.IsNotExist(err))
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithNotDir() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs := s.TemporalFilesystem()

	tmpDir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	repoDir := fs.Join(tmpDir, "repoDir")

	f, err := fs.Create(repoDir)
	s.NoError(err)
	s.Nil(f.Close())

	r, err := PlainCloneContext(ctx, fs.Join(fs.Root(), repoDir), &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	s.Nil(r)
	s.ErrorContains(err, "not a directory")

	fi, err := fs.Stat(repoDir)
	s.NoError(err)
	s.False(fi.IsDir())
}

func (s *RepositorySuite) TestPlainCloneContextFailedClonePreservesExistingDir() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fs := s.TemporalFilesystem()

	tmpDir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	repoDir := filepath.Join(tmpDir, "repoDir")
	err = fs.MkdirAll(repoDir, 0o777)
	s.NoError(err)

	dummyFile := filepath.Join(repoDir, "dummyFile")
	err = util.WriteFile(fs, dummyFile, []byte("dummyContent"), 0o644)
	s.NoError(err)

	r, err := PlainCloneContext(ctx, fs.Join(fs.Root(), repoDir), &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	s.Nil(r)
	s.ErrorIs(err, ErrTargetDirNotEmpty)

	_, err = fs.Stat(dummyFile)
	s.NoError(err)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistingOverExistingGitDirectory() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := s.T().TempDir()
	r, err := PlainInit(dir, false)
	s.NotNil(r)
	s.NoError(err)
	_ = r.Close()

	r, err = PlainCloneContext(ctx, dir, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	s.Nil(r)
	s.ErrorIs(err, ErrTargetDirNotEmpty)
}

func (s *RepositorySuite) TestPlainCloneWithRecurseSubmodules() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	fixtures.ByTag("submodule").Run(s.T(), func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, err := PlainClone(t.TempDir(), &CloneOptions{
			URL:               s.GetLocalRepositoryURL(f),
			RecurseSubmodules: DefaultSubmoduleRecursionDepth,
		})
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Len(t, cfg.Remotes, 1)
		assert.Len(t, cfg.Branches, 1)
		assert.Len(t, cfg.Submodules, 2)
	})
}

func (s *RepositorySuite) TestPlainCloneWithShallowSubmodules() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")

	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	fixtures.ByTag("submodule").Run(s.T(), func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		dir := t.TempDir()
		mainRepo, err := PlainClone(dir, &CloneOptions{
			URL:               s.GetLocalRepositoryURL(f),
			RecurseSubmodules: 1,
			ShallowSubmodules: true,
		})
		require.NoError(t, err)
		defer func() { _ = mainRepo.Close() }()

		mainWorktree, err := mainRepo.Worktree()
		require.NoError(t, err)

		submodule, err := mainWorktree.Submodule(primaryFixtureSubmoduleName(f))
		require.NoError(t, err)

		subRepo, err := submodule.Repository()
		require.NoError(t, err)
		defer subRepo.Close()

		lr, err := subRepo.Log(&LogOptions{})
		require.NoError(t, err)

		commitCount := 0
		for _, err := lr.Next(); err == nil; _, err = lr.Next() {
			commitCount++
		}
		require.NoError(t, err)
		assert.Equal(t, 1, commitCount)
	})
}

func (s *RepositorySuite) TestPlainCloneNoCheckout() {
	fixtures.ByTag("submodule").Run(s.T(), func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		dir := t.TempDir()

		r, err := PlainClone(dir, &CloneOptions{
			URL:               s.GetLocalRepositoryURL(f),
			NoCheckout:        true,
			RecurseSubmodules: DefaultSubmoduleRecursionDepth,
		})
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		h, err := r.Head()
		require.NoError(t, err)
		if f.Head != "" {
			assert.Equal(t, f.Head, h.Hash().String())
		} else {
			assert.False(t, h.Hash().IsZero(), "HEAD should not be empty")
		}

		fi, err := osfs.New(dir).ReadDir("")
		require.NoError(t, err)
		assert.Len(t, fi, 1) // .git
	})
}

func (s *RepositorySuite) TestFetch() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	s.NoError(err)
	s.Nil(r.Fetch(&FetchOptions{}))

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)

	_, err = r.Head()
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)

	branch, err := r.Reference("refs/remotes/origin/master", false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal(plumbing.HashReference, branch.Type())
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())
}

func (s *RepositorySuite) TestFetchContext() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	s.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.NotNil(r.FetchContext(ctx, &FetchOptions{}))
}

func (s *RepositorySuite) TestFetchWithFilters() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	s.NoError(err)

	err = r.Fetch(&FetchOptions{
		Filter: packp.FilterBlobNone(),
	})
	s.ErrorIs(err, transport.ErrFilterNotSupported)
}

func (s *RepositorySuite) TestFetchWithFiltersReal() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})
	s.NoError(err)
	err = r.Fetch(&FetchOptions{
		Filter: packp.FilterBlobNone(),
	})
	s.NoError(err)
	blob, err := r.BlobObject(plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492"))
	s.NotNil(err)
	s.Nil(blob)
}

func (s *RepositorySuite) TestCloneWithProgress() {
	s.T().Skip("Currently, go-git server-side implementation does not support writing" +
		"progress and sideband messages to the client. This means any tests that" +
		"use local repositories to test progress messages will fail.")
	fs := memfs.New()

	buf := bytes.NewBuffer(nil)
	r, err := Clone(memory.NewStorage(), fs, &CloneOptions{
		URL:      s.GetBasicLocalRepositoryURL(),
		Progress: buf,
	})

	s.NoError(err)
	defer func() { _ = r.Close() }()
	s.NotEqual(0, buf.Len())
}

func (s *RepositorySuite) TestCloneDeep() {
	fs := memfs.New()
	r, _ := Init(memory.NewStorage(), WithWorkTree(fs))
	defer func() { _ = r.Close() }()

	head, err := r.Head()
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
	s.Nil(head)

	err = r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)

	head, err = r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.SymbolicReference, head.Type())
	s.Equal("refs/heads/master", head.Target().String())

	branch, err := r.Reference(head.Target(), false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())

	branch, err = r.Reference("refs/remotes/origin/master", false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal(plumbing.HashReference, branch.Type())
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())

	fi, err := fs.ReadDir("")
	s.NoError(err)
	s.Len(fi, 8)
}

func (s *RepositorySuite) TestCloneConfig() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	head, err := r.Head()
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
	s.Nil(head)

	err = r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	cfg, err := r.Config()
	s.NoError(err)

	s.True(cfg.Core.IsBare)
	s.Len(cfg.Remotes, 1)
	s.Equal("origin", cfg.Remotes["origin"].Name)
	s.Len(cfg.Remotes["origin"].URLs, 1)
	s.Len(cfg.Branches, 1)
	s.Equal("master", cfg.Branches["master"].Name)
}

func (s *RepositorySuite) TestCloneSingleBranchAndNonHEAD() {
	s.testCloneSingleBranchAndNonHEADReference("refs/heads/branch")
}

func (s *RepositorySuite) TestCloneSingleBranchAndNonHEADAndNonFull() {
	s.testCloneSingleBranchAndNonHEADReference("branch")
}

func (s *RepositorySuite) testCloneSingleBranchAndNonHEADReference(ref string) {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	head, err := r.Head()
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
	s.Nil(head)

	err = r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
	})

	s.NoError(err)

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	s.Equal("branch", cfg.Branches["branch"].Name)
	s.Equal("origin", cfg.Branches["branch"].Remote)
	s.Equal(plumbing.ReferenceName("refs/heads/branch"), cfg.Branches["branch"].Merge)

	head, err = r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.SymbolicReference, head.Type())
	s.Equal("refs/heads/branch", head.Target().String())

	branch, err := r.Reference(head.Target(), false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", branch.Hash().String())

	branch, err = r.Reference("refs/remotes/origin/branch", false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal(plumbing.HashReference, branch.Type())
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", branch.Hash().String())
}

func (s *RepositorySuite) TestCloneSingleBranchHEADMain() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	head, err := r.Head()
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
	s.Nil(head)

	err = r.clone(context.Background(), &CloneOptions{
		URL:          s.GetLocalRepositoryURL(fixtures.ByTag("no-master-head").One()),
		SingleBranch: true,
	})

	s.NoError(err)

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	s.Equal("main", cfg.Branches["main"].Name)
	s.Equal("origin", cfg.Branches["main"].Remote)
	s.Equal(plumbing.ReferenceName("refs/heads/main"), cfg.Branches["main"].Merge)

	head, err = r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.SymbolicReference, head.Type())
	s.Equal("refs/heads/main", head.Target().String())

	branch, err := r.Reference(head.Target(), false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal("786dafbd351e587da1ae97e5fb9fbdf868b4a28f", branch.Hash().String())

	branch, err = r.Reference("refs/remotes/origin/HEAD", false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal(plumbing.HashReference, branch.Type())
	s.Equal("786dafbd351e587da1ae97e5fb9fbdf868b4a28f", branch.Hash().String())
}

func (s *RepositorySuite) TestCloneSingleBranch() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	head, err := r.Head()
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
	s.Nil(head)

	err = r.clone(context.Background(), &CloneOptions{
		URL:          s.GetBasicLocalRepositoryURL(),
		SingleBranch: true,
	})

	s.NoError(err)

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 1)
	s.Equal("master", cfg.Branches["master"].Name)
	s.Equal("origin", cfg.Branches["master"].Remote)
	s.Equal(plumbing.ReferenceName("refs/heads/master"), cfg.Branches["master"].Merge)

	head, err = r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.SymbolicReference, head.Type())
	s.Equal("refs/heads/master", head.Target().String())

	branch, err := r.Reference(head.Target(), false)
	s.NoError(err)
	s.NotNil(branch)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())
}

func (s *RepositorySuite) TestCloneSingleTag() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	err := r.clone(context.Background(), &CloneOptions{
		URL:           url,
		SingleBranch:  true,
		ReferenceName: plumbing.ReferenceName("refs/tags/commit-tag"),
	})
	s.NoError(err)

	branch, err := r.Reference("refs/tags/commit-tag", false)
	s.NoError(err)
	s.NotNil(branch)

	conf, err := r.Config()
	s.NoError(err)
	originRemote := conf.Remotes["origin"]
	s.NotNil(originRemote)
	s.Len(originRemote.Fetch, 1)
	s.Equal("+refs/tags/commit-tag:refs/tags/commit-tag", originRemote.Fetch[0].String())
}

func (s *RepositorySuite) TestCloneDetachedHEAD() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
	})
	s.NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.HashReference, head.Type())
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", head.Hash().String())

	count := 0
	objects, err := r.Objects()
	s.NoError(err)
	objects.ForEach(func(object.Object) error { count++; return nil })
	s.Equal(28, count)
}

func (s *RepositorySuite) TestCloneDetachedHEADAndSingle() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
		SingleBranch:  true,
	})
	s.NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.HashReference, head.Type())
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", head.Hash().String())

	count := 0
	objects, err := r.Objects()
	s.NoError(err)
	objects.ForEach(func(object.Object) error { count++; return nil })
	s.Equal(28, count)
}

func (s *RepositorySuite) TestCloneDetachedHEADAndShallow() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")
	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
		Depth:         1,
	})

	s.NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.HashReference, head.Type())
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", head.Hash().String())

	count := 0
	objects, err := r.Objects()
	s.NoError(err)
	objects.ForEach(func(object.Object) error { count++; return nil })
	s.Equal(15, count)
}

func (s *RepositorySuite) TestCloneDetachedHEADAnnotatedTag() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetLocalRepositoryURL(fixtures.ByTag("tags").One()),
		ReferenceName: plumbing.ReferenceName("refs/tags/annotated-tag"),
	})
	s.NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Branches, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.NotNil(head)
	s.Equal(plumbing.HashReference, head.Type())
	s.Equal("f7b877701fbf855b44c0a9e86f3fdce2c298b07f", head.Hash().String())

	count := 0
	objects, err := r.Objects()
	s.NoError(err)
	objects.ForEach(func(object.Object) error { count++; return nil })
	s.Equal(7, count)
}

func (s *RepositorySuite) TestCloneWithFilter() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()

	err := r.clone(context.Background(), &CloneOptions{
		URL:    "https://github.com/git-fixtures/basic.git",
		Filter: packp.FilterTreeDepth(0),
	})
	s.Require().NoError(err)
	blob, err := r.BlobObject(plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492"))
	s.Require().Error(err)
	s.Nil(blob)
}

func (s *RepositorySuite) TestPush() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	_, err = s.Repository.CreateRemote(&config.RemoteConfig{
		Name: "test",
		URLs: []string{url},
	})
	s.NoError(err)

	err = s.Repository.Push(&PushOptions{
		RemoteName: "test",
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/heads/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/heads/branch": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
	})

	AssertReferences(s.T(), s.Repository, map[string]string{
		"refs/remotes/test/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/remotes/test/branch": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
	})
}

func (s *RepositorySuite) TestPushContext() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)
	_ = server.Close()

	_, err = s.Repository.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{url},
	})
	s.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = s.Repository.PushContext(ctx, &PushOptions{
		RemoteName: "foo",
	})
	s.NotNil(err)
}

// installPreReceiveHook installs a pre-receive hook in the .git
// directory at path which prints message m before exiting
// successfully.
func installPreReceiveHook(s *RepositorySuite, fs billy.Filesystem, path, m string) {
	hooks := fs.Join(path, "hooks")
	err := fs.MkdirAll(hooks, 0o777)
	s.NoError(err)

	err = util.WriteFile(fs, fs.Join(hooks, "pre-receive"), preReceiveHook(m), 0o777)
	s.NoError(err)
}

func (s *RepositorySuite) TestPushWithProgress() {
	s.T().Skip("Currently, go-git server-side implementation does not support writing" +
		"progress and sideband messages to the client. This means any tests that" +
		"use local repositories to test progress messages will fail.")
	fs := s.TemporalFilesystem()

	path, err := util.TempDir(fs, "", "")
	s.NoError(err)

	url := fs.Join(fs.Root(), path)

	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	m := "Receiving..."
	installPreReceiveHook(s, fs, path, m)

	_, err = s.Repository.CreateRemote(&config.RemoteConfig{
		Name: "bar",
		URLs: []string{url},
	})
	s.NoError(err)

	var p bytes.Buffer
	err = s.Repository.Push(&PushOptions{
		RemoteName: "bar",
		Progress:   &p,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/heads/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/heads/branch": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
	})

	s.Equal([]byte(m), (&p).Bytes())
}

func (s *RepositorySuite) TestPushDepth() {
	server, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL:   server.wt.Root(),
		Depth: 1,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

	err = util.WriteFile(r.wt, "foo", nil, 0o755)
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	hash, err := w.Commit("foo", &CommitOptions{
		Author:    defaultSignature(),
		Committer: defaultSignature(),
	})
	s.NoError(err)

	err = r.Push(&PushOptions{})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/heads/master": hash.String(),
	})

	AssertReferences(s.T(), r, map[string]string{
		"refs/remotes/origin/master": hash.String(),
	})
}

func (s *RepositorySuite) TestPushNonExistentRemote() {
	srcFs, err := fixtures.Basic().One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	r, err := Open(sto, srcFs)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	err = r.Push(&PushOptions{RemoteName: "myremote"})
	s.ErrorContains(err, "remote not found")
}

func (s *RepositorySuite) TestLog() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	cIter, err := r.Log(&LogOptions{
		From: plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	})

	s.NoError(err)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		s.NoError(err)
		s.Equal(o, commit.Hash)
	}
	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *RepositorySuite) TestLogAll() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	rIter, err := r.Storer.IterReferences()
	s.NoError(err)

	refCount := 0
	err = rIter.ForEach(func(*plumbing.Reference) error {
		refCount++
		return nil
	})
	s.NoError(err)
	s.Equal(5, refCount)

	cIter, err := r.Log(&LogOptions{
		All: true,
	})
	s.NoError(err)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		s.NoError(err)
		s.Equal(o, commit.Hash)
	}
	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
	cIter.Close()
}

func (s *RepositorySuite) TestLogAllMissingReferences() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)
	err = r.Storer.RemoveReference(plumbing.HEAD)
	s.NoError(err)

	rIter, err := r.Storer.IterReferences()
	s.NoError(err)

	refCount := 0
	err = rIter.ForEach(func(*plumbing.Reference) error {
		refCount++
		return nil
	})
	s.NoError(err)
	s.Equal(4, refCount)

	err = r.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("DUMMY"), plumbing.NewHash("DUMMY")))
	s.NoError(err)

	rIter, err = r.Storer.IterReferences()
	s.NoError(err)

	refCount = 0
	err = rIter.ForEach(func(*plumbing.Reference) error {
		refCount++
		return nil
	})
	s.NoError(err)
	s.Equal(5, refCount)

	cIter, err := r.Log(&LogOptions{
		All: true,
	})
	s.NotNil(cIter)
	s.NoError(err)

	cCount := 0
	cIter.ForEach(func(*object.Commit) error {
		cCount++
		return nil
	})
	s.Equal(9, cCount)

	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
	cIter.Close()
}

func (s *RepositorySuite) TestLogAllOrderByTime() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	cIter, err := r.Log(&LogOptions{
		Order: LogOrderCommitterTime,
		All:   true,
	})
	s.NoError(err)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		s.NoError(err)
		s.Equal(o, commit.Hash)
	}
	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
	cIter.Close()
}

func (s *RepositorySuite) TestLogHead() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	cIter, err := r.Log(&LogOptions{})

	s.NoError(err)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		s.NoError(err)
		s.Equal(o, commit.Hash)
	}
	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *RepositorySuite) TestLogError() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	_, err = r.Log(&LogOptions{
		From: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	s.NotNil(err)
}

func (s *RepositorySuite) TestLogFileNext() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	fileName := "vendor/foo.go"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})

	s.NoError(err)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		s.NoError(err)
		s.Equal(o, commit.Hash)
	}
	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *RepositorySuite) TestLogFileForEach() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	fileName := "php/crappy.php"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})
	s.NoError(err)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		s.Equal(expectedCommitHash.String(), commit.Hash.String())
		expectedIndex++
		return nil
	})
	s.NoError(err)
	s.Equal(1, expectedIndex)
}

func (s *RepositorySuite) TestLogNonHeadFile() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	fileName := "README"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})
	s.NoError(err)
	defer cIter.Close()

	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *RepositorySuite) TestLogAllFileForEach() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	fileName := "README"
	cIter, err := r.Log(&LogOptions{FileName: &fileName, All: true})
	s.NoError(err)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		s.Equal(expectedCommitHash.String(), commit.Hash.String())
		expectedIndex++
		return nil
	})
	s.NoError(err)
	s.Equal(1, expectedIndex)
}

func (s *RepositorySuite) TestLogInvalidFile() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	// Throwing in a file that does not exist
	fileName := "vendor/foo12.go"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})
	// Not raising an error since `git log -- vendor/foo12.go` responds silently
	s.NoError(err)
	defer cIter.Close()

	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *RepositorySuite) TestLogFileInitialCommit() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	fileName := "LICENSE"
	cIter, err := r.Log(&LogOptions{
		Order:    LogOrderCommitterTime,
		FileName: &fileName,
	})
	s.NoError(err)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		s.Equal(expectedCommitHash.String(), commit.Hash.String())
		expectedIndex++
		return nil
	})
	s.NoError(err)
	s.Equal(1, expectedIndex)
}

func (s *RepositorySuite) TestLogFileWithOtherParamsFail() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	fileName := "vendor/foo.go"
	cIter, err := r.Log(&LogOptions{
		Order:    LogOrderCommitterTime,
		FileName: &fileName,
		From:     plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	s.NoError(err)
	defer cIter.Close()

	_, iterErr := cIter.Next()
	s.Equal(io.EOF, iterErr)
}

func (s *RepositorySuite) TestLogFileWithOtherParamsPass() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	fileName := "LICENSE"
	cIter, err := r.Log(&LogOptions{
		Order:    LogOrderCommitterTime,
		FileName: &fileName,
		From:     plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	s.NoError(err)
	commitVal, iterErr := cIter.Next()
	s.Equal(nil, iterErr)
	s.Equal("b029517f6300c2da0f4b651b8642506cd6aaf45d", commitVal.Hash.String())

	_, iterErr = cIter.Next()
	s.Equal(io.EOF, iterErr)
}

type mockErrCommitIter struct{}

func (m *mockErrCommitIter) Next() (*object.Commit, error) {
	return nil, errors.New("mock next error")
}

func (m *mockErrCommitIter) ForEach(func(*object.Commit) error) error {
	return errors.New("mock foreach error")
}

func (m *mockErrCommitIter) Close() {}

func (s *RepositorySuite) TestLogFileWithError() {
	fileName := "README"
	cIter := object.NewCommitFileIterFromIter(fileName, &mockErrCommitIter{}, false)
	defer cIter.Close()

	err := cIter.ForEach(func(*object.Commit) error {
		return nil
	})
	s.NotNil(err)
}

func (s *RepositorySuite) TestLogPathWithError() {
	fileName := "README"
	pathIter := func(path string) bool {
		return path == fileName
	}
	cIter := object.NewCommitPathIterFromIter(pathIter, &mockErrCommitIter{}, false)
	defer cIter.Close()

	err := cIter.ForEach(func(*object.Commit) error {
		return nil
	})
	s.NotNil(err)
}

func (s *RepositorySuite) TestLogPathRegexpWithError() {
	pathRE := regexp.MustCompile("R.*E")
	pathIter := func(path string) bool {
		return pathRE.MatchString(path)
	}
	cIter := object.NewCommitPathIterFromIter(pathIter, &mockErrCommitIter{}, false)
	defer cIter.Close()

	err := cIter.ForEach(func(*object.Commit) error {
		return nil
	})
	s.NotNil(err)
}

func (s *RepositorySuite) TestLogPathFilterRegexp() {
	pathRE := regexp.MustCompile(`.*\.go`)
	pathIter := func(path string) bool {
		return pathRE.MatchString(path)
	}

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	expectedCommitIDs := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"918c48b83bd081e863dbe1b80f8998f058cd8294",
	}
	commitIDs := []string{}

	cIter, err := r.Log(&LogOptions{
		PathFilter: pathIter,
		From:       plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	s.NoError(err)
	defer cIter.Close()

	cIter.ForEach(func(commit *object.Commit) error {
		commitIDs = append(commitIDs, commit.ID().String())
		return nil
	})
	s.Equal(
		strings.Join(expectedCommitIDs, ", "),
		strings.Join(commitIDs, ", "),
	)
}

func (s *RepositorySuite) TestLogLimitNext() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	since := time.Date(2015, 4, 1, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{Since: &since})

	s.NoError(err)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		s.NoError(err)
		s.Equal(o, commit.Hash)
	}
	_, err = cIter.Next()
	s.ErrorIs(err, io.EOF)
}

func (s *RepositorySuite) TestLogLimitForEach() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	since := time.Date(2015, 3, 31, 11, 54, 0, 0, time.UTC)
	until := time.Date(2015, 4, 1, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{Since: &since, Until: &until})
	s.NoError(err)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		s.Equal(expectedCommitHash.String(), commit.Hash.String())
		expectedIndex++
		return nil
	})
	s.NoError(err)
	s.Equal(1, expectedIndex)
}

func (s *RepositorySuite) TestLogAllLimitForEach() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	since := time.Date(2015, 3, 31, 11, 54, 0, 0, time.UTC)
	until := time.Date(2015, 4, 1, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{Since: &since, Until: &until, All: true})
	s.NoError(err)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		s.Equal(expectedCommitHash.String(), commit.Hash.String())
		expectedIndex++
		return nil
	})
	s.NoError(err)
	s.Equal(2, expectedIndex)
}

func (s *RepositorySuite) TestLogLimitWithOtherParamsFail() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	since := time.Date(2015, 3, 31, 11, 54, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{
		Order: LogOrderCommitterTime,
		Since: &since,
		From:  plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	s.NoError(err)
	defer cIter.Close()

	_, iterErr := cIter.Next()
	s.Equal(io.EOF, iterErr)
}

func (s *RepositorySuite) TestLogLimitWithOtherParamsPass() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	until := time.Date(2015, 3, 31, 11, 43, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{
		Order: LogOrderCommitterTime,
		Until: &until,
		From:  plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	s.NoError(err)
	defer cIter.Close()

	commitVal, iterErr := cIter.Next()
	s.Equal(nil, iterErr)
	s.Equal("b029517f6300c2da0f4b651b8642506cd6aaf45d", commitVal.Hash.String())

	_, iterErr = cIter.Next()
	s.Equal(io.EOF, iterErr)
}

func (s *RepositorySuite) TestConfigScoped() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	cfg, err := r.ConfigScoped(config.LocalScope)
	s.NoError(err)
	s.Equal("", cfg.User.Email)

	cfg, err = r.ConfigScoped(config.SystemScope)
	s.NoError(err)
	s.NotEqual("", cfg.User.Email)
}

func (s *RepositorySuite) TestCommit() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	hash := plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")
	commit, err := r.CommitObject(hash)
	s.NoError(err)

	s.False(commit.Hash.IsZero())
	s.Equal(commit.ID(), commit.Hash)
	s.Equal(hash, commit.Hash)
	s.Equal(plumbing.CommitObject, commit.Type())

	tree, err := commit.Tree()
	s.NoError(err)
	s.False(tree.Hash.IsZero())

	s.Equal("daniel@lordran.local", commit.Author.Email)
}

func (s *RepositorySuite) TestCommits() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	count := 0
	commits, err := r.CommitObjects()
	s.NoError(err)
	for {
		commit, err := commits.Next()
		if err != nil {
			break
		}

		count++
		s.False(commit.Hash.IsZero())
		s.Equal(commit.ID(), commit.Hash)
		s.Equal(plumbing.CommitObject, commit.Type())
	}

	s.Equal(9, count)
}

func (s *RepositorySuite) TestBlob() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	s.NoError(err)

	blob, err := r.BlobObject(plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"))
	s.NotNil(err)
	s.Nil(blob)

	blobHash := plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")
	blob, err = r.BlobObject(blobHash)
	s.NoError(err)

	s.False(blob.Hash.IsZero())
	s.Equal(blob.ID(), blob.Hash)
	s.Equal(blobHash, blob.Hash)
	s.Equal(plumbing.BlobObject, blob.Type())
}

func (s *RepositorySuite) TestBlobs() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	count := 0
	blobs, err := r.BlobObjects()
	s.NoError(err)
	for {
		blob, err := blobs.Next()
		if err != nil {
			break
		}

		count++
		s.False(blob.Hash.IsZero())
		s.Equal(blob.ID(), blob.Hash)
		s.Equal(plumbing.BlobObject, blob.Type())
	}

	s.Equal(10, count)
}

func (s *RepositorySuite) TestTagObject() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	hash := plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc")
	tag, err := r.TagObject(hash)
	s.NoError(err)

	s.False(tag.Hash.IsZero())
	s.Equal(hash, tag.Hash)
	s.Equal(plumbing.TagObject, tag.Type())
}

func (s *RepositorySuite) TestTags() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	count := 0
	tags, err := r.Tags()
	s.NoError(err)

	tags.ForEach(func(tag *plumbing.Reference) error {
		count++
		s.False(tag.Hash().IsZero())
		s.True(tag.Name().IsTag())
		return nil
	})

	s.Equal(5, count)
}

func (s *RepositorySuite) TestCreateTagLightweight() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	expected, err := r.Head()
	s.NoError(err)

	ref, err := r.CreateTag("foobar", expected.Hash(), nil)
	s.NoError(err)
	s.NotNil(ref)

	actual, err := r.Tag("foobar")
	s.NoError(err)

	s.Equal(actual.Hash(), expected.Hash())
}

func (s *RepositorySuite) TestCreateTagLightweightExists() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	expected, err := r.Head()
	s.NoError(err)

	ref, err := r.CreateTag("lightweight-tag", expected.Hash(), nil)
	s.Nil(ref)
	s.ErrorIs(err, ErrTagExists)
}

func (s *RepositorySuite) TestCreateTagAnnotated() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	h, err := r.Head()
	s.NoError(err)

	expectedHash := h.Hash()

	ref, err := r.CreateTag("foobar", expectedHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	s.NoError(err)

	tag, err := r.Tag("foobar")
	s.NoError(err)

	obj, err := r.TagObject(tag.Hash())
	s.NoError(err)

	s.Equal(tag, ref)
	s.Equal(ref.Hash(), obj.Hash)
	s.Equal(plumbing.TagObject, obj.Type())
	s.Equal(expectedHash, obj.Target)
}

func (s *RepositorySuite) TestCreateTagAnnotatedBadOpts() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	h, err := r.Head()
	s.NoError(err)

	expectedHash := h.Hash()

	ref, err := r.CreateTag("foobar", expectedHash, &CreateTagOptions{})
	s.Nil(ref)
	s.ErrorIs(err, ErrMissingMessage)
}

func (s *RepositorySuite) TestCreateTagAnnotatedBadHash() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	ref, err := r.CreateTag("foobar", plumbing.ZeroHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	s.Nil(ref)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestCreateTagCanonicalize() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	h, err := r.Head()
	s.NoError(err)

	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "\n\nfoo bar baz qux\n\nsome message here",
	})
	s.NoError(err)

	tag, err := r.Tag("foobar")
	s.NoError(err)

	obj, err := r.TagObject(tag.Hash())
	s.NoError(err)

	// Assert the new canonicalized message.
	s.Equal("foo bar baz qux\n\nsome message here\n", obj.Message)
}

func (s *RepositorySuite) TestTagLightweight() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	expected := plumbing.NewHash("f7b877701fbf855b44c0a9e86f3fdce2c298b07f")

	tag, err := r.Tag("lightweight-tag")
	s.NoError(err)

	actual := tag.Hash()
	s.Equal(actual, expected)
}

func (s *RepositorySuite) TestTagLightweightMissingTag() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	tag, err := r.Tag("lightweight-tag-tag")
	s.Nil(tag)
	s.ErrorIs(err, ErrTagNotFound)
}

func (s *RepositorySuite) TestDeleteTag() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	err = r.DeleteTag("lightweight-tag")
	s.NoError(err)

	_, err = r.Tag("lightweight-tag")
	s.ErrorIs(err, ErrTagNotFound)
}

func (s *RepositorySuite) TestDeleteTagMissingTag() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	err = r.DeleteTag("lightweight-tag-tag")
	s.ErrorIs(err, ErrTagNotFound)
}

func (s *RepositorySuite) TestDeleteTagAnnotated() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	fs := s.TemporalFilesystem()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, _ := Init(fss)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	ref, err := r.Tag("annotated-tag")
	s.NotNil(ref)
	s.NoError(err)

	obj, err := r.TagObject(ref.Hash())
	s.NotNil(obj)
	s.NoError(err)

	err = r.DeleteTag("annotated-tag")
	s.NoError(err)

	_, err = r.Tag("annotated-tag")
	s.ErrorIs(err, ErrTagNotFound)

	// Run a prune (and repack, to ensure that we are GCing everything regardless
	// of the fixture in use) and try to get the tag object again.
	//
	// The repo needs to be re-opened after the repack.
	err = r.Prune(PruneOptions{Handler: r.DeleteObject})
	s.NoError(err)

	err = r.RepackObjects(&RepackConfig{})
	s.NoError(err)

	_ = r.Close()
	r, err = PlainOpen(fs.Root())
	s.NotNil(r)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	// Now check to see if the GC was effective in removing the tag object.
	obj, err = r.TagObject(ref.Hash())
	s.Nil(obj)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestDeleteTagAnnotatedUnpacked() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	fs := s.TemporalFilesystem()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, _ := Init(fss)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)
	defer func() { _ = r.Close() }()

	// Create a tag for the deletion test. This ensures that the ultimate loose
	// object will be unpacked (as we aren't doing anything that should pack it),
	// so that we can effectively test that a prune deletes it, without having to
	// resort to a repack.
	h, err := r.Head()
	s.NoError(err)

	expectedHash := h.Hash()

	ref, err := r.CreateTag("foobar", expectedHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	s.NoError(err)

	tag, err := r.Tag("foobar")
	s.NoError(err)

	obj, err := r.TagObject(tag.Hash())
	s.NotNil(obj)
	s.NoError(err)

	err = r.DeleteTag("foobar")
	s.NoError(err)

	_, err = r.Tag("foobar")
	s.ErrorIs(err, ErrTagNotFound)

	// As mentioned, only run a prune. We are not testing for packed objects
	// here.
	err = r.Prune(PruneOptions{Handler: r.DeleteObject})
	s.NoError(err)

	// Now check to see if the GC was effective in removing the tag object.
	obj, err = r.TagObject(ref.Hash())
	s.Nil(obj)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestInvalidTagName() {
	r, err := Init(memory.NewStorage())
	s.NoError(err)
	defer func() { _ = r.Close() }()
	for i, name := range []string{
		"",
		"foo bar",
		"foo\tbar",
		"foo\nbar",
	} {
		_, err = r.CreateTag(name, plumbing.ZeroHash, nil)
		s.Error(err, fmt.Sprintf("case %d %q", i, name))
	}
}

func (s *RepositorySuite) TestBranches() {
	f := fixtures.ByURL("https://github.com/git-fixtures/root-references.git").One()
	dotgit1, err := f.DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit1, cache.NewObjectLRUDefault())
	dotgit2, err := f.DotGit()
	s.Require().NoError(err)
	r, err := Open(sto, dotgit2)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	count := 0
	branches, err := r.Branches()
	s.NoError(err)

	branches.ForEach(func(branch *plumbing.Reference) error {
		count++
		s.False(branch.Hash().IsZero())
		s.True(branch.Name().IsBranch())
		return nil
	})

	s.Equal(8, count)
}

func (s *RepositorySuite) TestNotes() {
	// TODO add fixture with Notes
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	count := 0
	notes, err := r.Notes()
	s.NoError(err)

	notes.ForEach(func(note *plumbing.Reference) error {
		count++
		s.False(note.Hash().IsZero())
		s.True(note.Name().IsNote())
		return nil
	})

	s.Equal(0, count)
}

func (s *RepositorySuite) TestTree() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	s.NoError(err)

	invalidHash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tree, err := r.TreeObject(invalidHash)
	s.Nil(tree)
	s.NotNil(err)

	hash := plumbing.NewHash("dbd3641b371024f44d0e469a9c8f5457b0660de1")
	tree, err = r.TreeObject(hash)
	s.NoError(err)

	s.False(tree.Hash.IsZero())
	s.Equal(tree.ID(), tree.Hash)
	s.Equal(hash, tree.Hash)
	s.Equal(plumbing.TreeObject, tree.Type())
	s.NotEqual(0, len(tree.Entries))
}

func (s *RepositorySuite) TestTrees() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	count := 0
	trees, err := r.TreeObjects()
	s.NoError(err)
	for {
		tree, err := trees.Next()
		if err != nil {
			break
		}

		count++
		s.False(tree.Hash.IsZero())
		s.Equal(tree.ID(), tree.Hash)
		s.Equal(plumbing.TreeObject, tree.Type())
		s.NotEqual(0, len(tree.Entries))
	}

	s.Equal(12, count)
}

func (s *RepositorySuite) TestTagObjects() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	count := 0
	tags, err := r.TagObjects()
	s.NoError(err)

	tags.ForEach(func(tag *object.Tag) error {
		count++

		s.False(tag.Hash.IsZero())
		s.Equal(plumbing.TagObject, tag.Type())
		return nil
	})

	refs, _ := r.References()
	refs.ForEach(func(*plumbing.Reference) error {
		return nil
	})

	s.Equal(4, count)
}

func (s *RepositorySuite) TestCommitIterClosePanic() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	commits, err := r.CommitObjects()
	s.NoError(err)
	commits.Close()
}

func (s *RepositorySuite) TestRef() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	ref, err := r.Reference(plumbing.HEAD, false)
	s.NoError(err)
	s.Equal(plumbing.HEAD, ref.Name())

	ref, err = r.Reference(plumbing.HEAD, true)
	s.NoError(err)
	s.Equal(plumbing.ReferenceName("refs/heads/master"), ref.Name())
}

func (s *RepositorySuite) TestRefs() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	s.NoError(err)

	iter, err := r.References()
	s.NoError(err)
	s.NotNil(iter)
}

func (s *RepositorySuite) TestObject() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	o, err := r.Object(plumbing.CommitObject, hash)
	s.NoError(err)

	s.False(o.ID().IsZero())
	s.Equal(plumbing.CommitObject, o.Type())
}

func (s *RepositorySuite) TestObjects() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	count := 0
	objects, err := r.Objects()
	s.NoError(err)
	for {
		o, err := objects.Next()
		if err != nil {
			break
		}

		count++
		s.False(o.ID().IsZero())
		s.NotEqual(plumbing.AnyObject, o.Type())
	}

	s.Equal(31, count)
}

func (s *RepositorySuite) TestObjectNotFound() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	hash := plumbing.NewHash("0a3fb06ff80156fb153bcdcc58b5e16c2d27625c")
	tag, err := r.Object(plumbing.TagObject, hash)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
	s.Nil(tag)
}

func (s *RepositorySuite) TestWorktree() {
	def := memfs.New()
	r, _ := Init(memory.NewStorage(), WithWorkTree(def))
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.NoError(err)
	s.Equal(def, w.Filesystem())
}

func (s *RepositorySuite) TestWorktreeBare() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.ErrorIs(err, ErrIsBareRepository)
	s.Nil(w)
}

func (s *RepositorySuite) TestResolveRevision() {
	f := fixtures.ByURL("https://github.com/git-fixtures/basic.git").One()
	dotgit1, err := f.DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit1, cache.NewObjectLRUDefault())
	dotgit2, err := f.DotGit()
	s.Require().NoError(err)
	r, err := Open(sto, dotgit2)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	datas := map[string]string{
		"HEAD":                       "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"heads/master":               "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"heads/master~1":             "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"refs/heads/master":          "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/heads/master~2^^~":     "b029517f6300c2da0f4b651b8642506cd6aaf45d",
		"refs/tags/v1.0.0":           "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/remotes/origin/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/remotes/origin/HEAD":   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"HEAD~2^^~":                  "b029517f6300c2da0f4b651b8642506cd6aaf45d",
		"HEAD~3^2":                   "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"HEAD~3^2^0":                 "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"HEAD~2^{/binary file}":      "35e85108805c84807bc66a02d91535e1e24b38b9",
		"HEAD~^{/!-some}":            "1669dce138d9b841a518c64b10914d88f5e488ea",
		"master":                     "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"branch":                     "e8d3ffab552895c19b9fcf7aa264d277cde33881",
		"v1.0.0":                     "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"branch~1":                   "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"v1.0.0~1":                   "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"master~1":                   "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"918c48b83bd081e863dbe1b80f8998f058cd8294": "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"918c48b": "918c48b83bd081e863dbe1b80f8998f058cd8294", // odd number of hex digits
	}

	for rev, hash := range datas {
		h, err := r.ResolveRevision(plumbing.Revision(rev))

		s.NoError(err, fmt.Sprintf("while checking %s", rev))
		s.Equal(hash, h.String(), fmt.Sprintf("while checking %s", rev))
	}
}

func (s *RepositorySuite) TestResolveRevisionAnnotated() {
	f := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One()
	dotgit1, err := f.DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit1, cache.NewObjectLRUDefault())
	dotgit2, err := f.DotGit()
	s.Require().NoError(err)
	r, err := Open(sto, dotgit2)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	datas := map[string]string{
		"refs/tags/annotated-tag":                  "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		"b742a2a9fa0afcfa9a6fad080980fbc26b007c69": "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
	}

	for rev, hash := range datas {
		h, err := r.ResolveRevision(plumbing.Revision(rev))

		s.NoError(err, fmt.Sprintf("while checking %s", rev))
		s.Equal(hash, h.String(), fmt.Sprintf("while checking %s", rev))
	}
}

func (s *RepositorySuite) TestResolveRevisionWithErrors() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/basic.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	headRef, err := r.Head()
	s.NoError(err)

	ref := plumbing.NewHashReference("refs/heads/918c48b83bd081e863dbe1b80f8998f058cd8294", headRef.Hash())
	err = r.Storer.SetReference(ref)
	s.NoError(err)

	datas := map[string]string{
		"efs/heads/master~": "reference not found",
		"HEAD^3":            `Revision invalid : "3" found must be 0, 1 or 2 after "^"`,
		"HEAD^{/whatever}":  `no commit message match regexp: "whatever"`,
		"4e1243bd22c66e76c2ba9eddc1f91394e57f9f83": "reference not found",
	}

	for rev, rerr := range datas {
		_, err := r.ResolveRevision(plumbing.Revision(rev))
		s.NotNil(err)
		s.Equal(rerr, err.Error())
	}
}

func (s *RepositorySuite) testRepackObjects(deleteTime time.Time, expectedPacks int) {
	srcFs, err := fixtures.ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	var sto storage.Storer = filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	los := sto.(storer.LooseObjectStorer)
	s.NotNil(los)

	numLooseStart := 0
	err = los.ForEachObjectHash(func(_ plumbing.Hash) error {
		numLooseStart++
		return nil
	})
	s.NoError(err)
	s.True(numLooseStart > 0)

	pos := sto.(storer.PackedObjectStorer)
	s.NotNil(los)

	packs, err := pos.ObjectPacks()
	s.NoError(err)
	numPacksStart := len(packs)
	s.True(numPacksStart > 1)

	r, err := Open(sto, srcFs)
	s.NoError(err)
	s.NotNil(r)
	defer func() { _ = r.Close() }()

	err = r.RepackObjects(&RepackConfig{
		OnlyDeletePacksOlderThan: deleteTime,
	})
	s.NoError(err)

	numLooseEnd := 0
	err = los.ForEachObjectHash(func(_ plumbing.Hash) error {
		numLooseEnd++
		return nil
	})
	s.NoError(err)
	s.Equal(0, numLooseEnd)

	packs, err = pos.ObjectPacks()
	s.NoError(err)
	numPacksEnd := len(packs)
	s.Equal(expectedPacks, numPacksEnd)
}

func (s *RepositorySuite) TestRepackObjects() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	s.testRepackObjects(time.Time{}, 1)
}

func (s *RepositorySuite) TestRepackObjectsWithNoDelete() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	s.testRepackObjects(time.Unix(0, 1), 3)
}

func ExecuteOnPath(t *testing.T, path string, cmds ...string) error {
	for _, cmd := range cmds {
		err := executeOnPath(path, cmd)
		assert.NoError(t, err)
	}

	return nil
}

func executeOnPath(path, cmd string) error {
	args := strings.Split(cmd, " ")
	c := exec.Command(args[0], args[1:]...)
	c.Dir = path
	c.Env = os.Environ()

	buf := bytes.NewBuffer(nil)
	c.Stderr = buf
	c.Stdout = buf

	return c.Run()
}

func (s *RepositorySuite) TestBrokenMultipleShallowFetch() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	s.NoError(err)

	s.NoError(r.Fetch(&FetchOptions{
		Depth:    2,
		RefSpecs: []config.RefSpec{config.RefSpec("refs/heads/master:refs/heads/master")},
	}))

	shallows, err := r.Storer.Shallow()
	s.NoError(err)
	s.Len(shallows, 1)

	ref, err := r.Reference("refs/heads/master", true)
	s.NoError(err)
	cobj, err := r.CommitObject(ref.Hash())
	s.NoError(err)
	s.NotNil(cobj)
	err = object.NewCommitPreorderIter(cobj, nil, nil).ForEach(func(c *object.Commit) error {
		for _, ph := range c.ParentHashes {
			if slices.Contains(shallows, ph) {
				return storer.ErrStop
			}
		}

		return nil
	})
	s.NoError(err)

	s.NoError(r.Fetch(&FetchOptions{
		Depth:    5,
		RefSpecs: []config.RefSpec{config.RefSpec("refs/heads/*:refs/heads/*")},
	}))

	shallows, err = r.Storer.Shallow()
	s.NoError(err)
	s.Len(shallows, 3)

	ref, err = r.Reference("refs/heads/master", true)
	s.NoError(err)
	cobj, err = r.CommitObject(ref.Hash())
	s.NoError(err)
	s.NotNil(cobj)
	err = object.NewCommitPreorderIter(cobj, nil, nil).ForEach(func(c *object.Commit) error {
		for _, ph := range c.ParentHashes {
			if slices.Contains(shallows, ph) {
				return storer.ErrStop
			}
		}

		return nil
	})
	s.NoError(err)
}

func (s *RepositorySuite) TestDotGitToOSFilesystemsInvalidPath() {
	_, _, err := dotGitToOSFilesystems("\000", false)
	s.NotNil(err)
}

func (s *RepositorySuite) TestIssue674() {
	r, _ := Init(memory.NewStorage())
	defer func() { _ = r.Close() }()
	h, err := r.ResolveRevision(plumbing.Revision(""))

	s.NotNil(err)
	s.NotNil(h)
	s.True(h.IsZero())
}

func BenchmarkObjects(b *testing.B) {
	for _, f := range fixtures.ByTag("packfile") {
		if f.DotGitHash == "" {
			continue
		}

		b.Run(f.URL, func(b *testing.B) {
			fs, err := f.DotGit()
			if err != nil {
				b.Fatal(err)
			}
			st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

			worktree, err := fs.Chroot(filepath.Dir(fs.Root()))
			if err != nil {
				b.Fatal(err)
			}

			repo, err := Open(st, worktree)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = repo.Close() }()

			for b.Loop() {
				iter, err := repo.Objects()
				if err != nil {
					b.Fatal(err)
				}

				for {
					_, err := iter.Next()
					if err == io.EOF {
						break
					}

					if err != nil {
						b.Fatal(err)
					}
				}

				iter.Close()
			}
		})
	}
}

func BenchmarkPlainClone(b *testing.B) {
	b.StopTimer()
	clone := func(b *testing.B) {
		r, err := PlainClone(b.TempDir(), &CloneOptions{
			URL:          "https://github.com/go-git/go-git.git",
			Depth:        1,
			Tags:         plumbing.NoTags,
			SingleBranch: true,
			Bare:         true,
		})
		if err != nil {
			b.Error(err)
		}
		if r != nil {
			defer func() { _ = r.Close() }()
		}
	}

	// Warm-up as the initial clone could have a higher cost which
	// may skew results.
	clone(b)

	b.StartTimer()
	for b.Loop() {
		clone(b)
	}
}

func TestCreateTagSignerSelection(t *testing.T) { //nolint:paralleltest // modifies global plugin state
	tests := []struct {
		name           string
		registerPlugin bool
		optionsSigner  Signer
		tagSignGpg     config.OptBool
		wantErr        string
		wantSignature  bool
		wantPluginUsed bool
		wantOptionUsed bool
	}{
		{
			name:          "no signer at all produces unsigned tag",
			tagSignGpg:    config.OptBoolFalse,
			wantSignature: false,
		},
		{
			name:           "CreateTagOptions.Signer works without plugin registered",
			tagSignGpg:     config.OptBoolFalse,
			optionsSigner:  &mockSigner{},
			wantSignature:  true,
			wantOptionUsed: true,
		},
		{
			name:           "plugin signer is used when CreateTagOptions.Signer is nil",
			registerPlugin: true,
			tagSignGpg:     config.OptBoolTrue,
			wantSignature:  true,
			wantPluginUsed: true,
		},
		{
			name:           "plugin signer is ignored if tag.signGpg=false",
			registerPlugin: true,
			tagSignGpg:     config.OptBoolFalse,
			wantPluginUsed: false,
		},
		{
			name:       "error if tag.signGpg=true and no plugin registered",
			tagSignGpg: config.OptBoolTrue,
			wantErr:    "cannot auto-sign tag: disable tag.gpgSign or register an ObjectSigner plugin",
		},
		{
			name:           "CreateTagOptions.Signer takes precedence over plugin",
			registerPlugin: true,
			tagSignGpg:     config.OptBoolTrue,
			optionsSigner:  &mockSigner{},
			wantSignature:  true,
			wantOptionUsed: true,
		},
	}

	for _, tt := range tests { //nolint:paralleltest // modifies global plugin state
		t.Run(tt.name, func(t *testing.T) {
			resetPluginEntry("object-signer")
			t.Cleanup(func() { resetPluginEntry("object-signer") })

			// Create a repo with an initial commit to tag.
			// This must happen before registering the plugin signer,
			// otherwise buildCommitObject would call the plugin signer.
			fs := memfs.New()
			r, err := Init(memory.NewStorage(), WithWorkTree(fs))
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			cfg, err := r.Config()
			require.NoError(t, err)

			cfg.Tag.GpgSign = tt.tagSignGpg
			err = r.SetConfig(cfg)
			require.NoError(t, err)

			w, err := r.Worktree()
			require.NoError(t, err)

			util.WriteFile(fs, "file.txt", []byte("content"), 0o644)
			_, err = w.Add("file.txt")
			require.NoError(t, err)

			commitHash, err := w.Commit("initial commit\n", &CommitOptions{
				Author: defaultSignature(),
			})
			require.NoError(t, err)

			var pluginSigner *mockSigner
			if tt.registerPlugin {
				pluginSigner = &mockSigner{}
				err = plugin.Register(plugin.ObjectSigner(), func() plugin.Signer { return pluginSigner })
				require.NoError(t, err)
			}

			_, err = r.CreateTag("test-tag", commitHash, &CreateTagOptions{
				Tagger:  defaultSignature(),
				Message: "tag message",
				Signer:  tt.optionsSigner,
			})
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
				return // no need to carry on
			}

			tagRef, err := r.Tag("test-tag")
			require.NoError(t, err)

			tagObj, err := r.TagObject(tagRef.Hash())
			require.NoError(t, err)

			if tt.wantSignature {
				assert.NotEmpty(t, tagObj.Signature)
			} else {
				assert.Empty(t, tagObj.Signature)
			}

			if tt.wantPluginUsed {
				require.NotNil(t, pluginSigner)
				assert.True(t, pluginSigner.called, "expected plugin signer to be called")
			}

			if tt.wantOptionUsed {
				optSigner, ok := tt.optionsSigner.(*mockSigner)
				require.True(t, ok)
				assert.True(t, optSigner.called, "expected options signer to be called")
			}

			if pluginSigner != nil && tt.optionsSigner != nil {
				assert.False(t, pluginSigner.called,
					"plugin signer should not be called when CreateTagOptions.Signer is set")
			}
		})
	}
}

func TestRepository_Archive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      ArchiveOptions
		wantErr   string
		wantRead  string // if non-empty, reading stream should fail containing this substring
		wantFiles bool   // if true, archive should contain at least one file
		wantCheck func(t *testing.T, data []byte)
	}{
		{
			name:      "tar format",
			opts:      ArchiveOptions{Format: "tar", Treeish: "master"},
			wantFiles: true,
		},
		{
			name:      "tar.gz format",
			opts:      ArchiveOptions{Format: "tar.gz", Treeish: "master"},
			wantFiles: true,
		},
		{
			name:      "tgz format",
			opts:      ArchiveOptions{Format: "tgz", Treeish: "master"},
			wantFiles: true,
		},
		{
			name:      "zip format",
			opts:      ArchiveOptions{Format: "zip", Treeish: "master"},
			wantFiles: true,
		},
		{
			name: "tar with prefix",
			opts: ArchiveOptions{Format: "tar", Treeish: "master", Prefix: "myproject/"},
			wantCheck: func(t *testing.T, data []byte) {
				tr := tar.NewReader(bytes.NewReader(data))
				for {
					hdr, err := tr.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					if hdr.Typeflag == tar.TypeXGlobalHeader {
						continue
					}
					assert.True(t, strings.HasPrefix(hdr.Name, "myproject/"),
						"expected prefix myproject/, got %s", hdr.Name)
				}
			},
		},
		{
			name:      "tar with path filter",
			opts:      ArchiveOptions{Format: "tar", Treeish: "master", Paths: []string{".gitignore"}},
			wantFiles: true,
		},
		{
			name:      "tar from tag",
			opts:      ArchiveOptions{Format: "tar", Treeish: "v1.0.0"},
			wantFiles: true,
		},
		{
			name: "tar contains commit ID in PAX header",
			opts: ArchiveOptions{Format: "tar", Treeish: "master"},
			wantCheck: func(t *testing.T, data []byte) {
				commitID, err := archivePkg.GetTarCommitID(bytes.NewReader(data))
				require.NoError(t, err)
				assert.NotNil(t, commitID)
			},
		},
		{
			name: "zip contains commit ID as comment",
			opts: ArchiveOptions{Format: "zip", Treeish: "master"},
			wantCheck: func(t *testing.T, data []byte) {
				zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				require.NoError(t, err)
				assert.NotEmpty(t, zr.Comment)
			},
		},
		{
			name:    "invalid format",
			opts:    ArchiveOptions{Format: "rar", Treeish: "master"},
			wantErr: "unsupported archive format",
		},
		{
			name:    "empty tree-ish",
			opts:    ArchiveOptions{Format: "tar", Treeish: ""},
			wantErr: "tree-ish is required",
		},
		{
			name:    "invalid prefix",
			opts:    ArchiveOptions{Format: "tar", Treeish: "master", Prefix: "../../etc/"},
			wantErr: "invalid archive prefix",
		},
		{
			name:    "nonexistent ref",
			opts:    ArchiveOptions{Format: "tar", Treeish: "nonexistent-branch"},
			wantErr: "cannot resolve",
		},
		{
			name:     "pathspec no match",
			opts:     ArchiveOptions{Format: "tar", Treeish: "master", Paths: []string{"nonexistent/path/xyzzy"}},
			wantRead: "pathspec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := setupArchiveRepo(t)
			defer func() { _ = r.Close() }()

			rc, err := r.Archive(&tt.opts)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { rc.Close() })

			data, err := io.ReadAll(rc)
			if tt.wantRead != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantRead)
				return
			}
			require.NoError(t, err)

			if tt.wantCheck != nil {
				tt.wantCheck(t, data)
				return
			}

			if tt.wantFiles {
				names := archiveFileNames(t, tt.opts.Format, data)
				assert.Greater(t, len(names), 0, "archive should contain files")
			}
		})
	}
}

func TestRepository_ArchiveContext_Cancelled(t *testing.T) {
	t.Parallel()

	r := setupArchiveRepo(t)
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	ar, err := r.ArchiveContext(ctx, &ArchiveOptions{
		Format:  "tar",
		Treeish: "master",
	})
	assert.NoError(t, err)
	t.Cleanup(func() { ar.Close() })
	_, err = io.Copy(io.Discard, ar)
	assert.ErrorIs(t, err, context.Canceled)
}

// archiveFileNames extracts file names from an archive based on format.
func archiveFileNames(t *testing.T, format string, data []byte) []string {
	t.Helper()

	var names []string

	switch format {
	case "zip":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
		for _, f := range zr.File {
			names = append(names, f.Name)
		}

	case "tar":
		tr := tar.NewReader(bytes.NewReader(data))
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if hdr.Typeflag == tar.TypeXGlobalHeader {
				continue
			}
			names = append(names, hdr.Name)
		}

	default: // tar.gz, tgz
		gr, err := gzip.NewReader(bytes.NewReader(data))
		require.NoError(t, err)
		tr := tar.NewReader(gr)
		defer gr.Close()
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if hdr.Typeflag == tar.TypeXGlobalHeader {
				continue
			}
			names = append(names, hdr.Name)
		}
	}

	return names
}

// setupArchiveRepo creates a repository from the basic fixture suitable for
// archive tests. It returns an opened *Repository.
func setupArchiveRepo(t *testing.T) *Repository {
	t.Helper()
	f := fixtures.Basic().One()
	dotgit, err := f.DotGit()
	require.NoError(t, err)
	st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	r, err := Open(st, memfs.New())
	require.NoError(t, err)
	return r
}
