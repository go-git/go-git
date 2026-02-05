package git

import (
	"bytes"
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

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	openpgperr "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
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
	_, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()),
		WithDefaultBranch("foo"),
	)
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

	r, err = Init(st)
	s.ErrorIs(err, ErrTargetDirNotEmpty)
	s.Nil(r)
}

func (s *RepositorySuite) TestOpen() {
	st := memory.NewStorage()

	r, err := Init(st, WithWorkTree(memfs.New()))
	s.NoError(err)
	s.NotNil(r)

	r, err = Open(st, memfs.New())
	s.NoError(err)
	s.NotNil(r)
}

func (s *RepositorySuite) TestOpenBare() {
	st := memory.NewStorage()

	r, err := Init(st)
	s.NoError(err)
	s.NotNil(r)

	r, err = Open(st, nil)
	s.NoError(err)
	s.NotNil(r)
}

func (s *RepositorySuite) TestOpenBareMissingWorktree() {
	st := memory.NewStorage()

	r, err := Init(st, WithWorkTree(memfs.New()))
	s.NoError(err)
	s.NotNil(r)

	r, err = Open(st, nil)
	s.NoError(err)
	s.NotNil(r)
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

	remotes, err := r.Remotes()
	s.NoError(err)
	s.Len(remotes, 1)
}

func TestCloneAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag        string
		format     formatcfg.ObjectFormat
		refs       int
		plainClone bool
	}{
		{tag: ".git-sha256", format: formatcfg.SHA256, refs: 4},
		{tag: ".git", format: formatcfg.UnsetObjectFormat, refs: 11},
		{tag: ".git-sha256", format: formatcfg.SHA256, refs: 4, plainClone: true},
		{tag: ".git", format: formatcfg.UnsetObjectFormat, refs: 11, plainClone: true},
	}

	for _, tc := range tests {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			f := fixtures.ByTag(tc.tag).One()

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

				remotes, err := r.Remotes()
				require.NoError(t, err)
				assert.Len(t, remotes, 1)

				iter, err := r.References()
				require.NoError(t, err)

				refs := 0
				iter.ForEach(func(r *plumbing.Reference) error {
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
		wantErr      bool
	}{
		{
			name:         "unset client format cannot fetch sha256",
			clientFormat: formatcfg.UnsetObjectFormat,
			serverTag:    ".git-sha256",
			wantErr:      true,
		},
		{
			name:         "sha1 client cannot fetch sha256",
			clientFormat: formatcfg.SHA1,
			serverTag:    ".git-sha256",
			wantErr:      true,
		},
		{
			name:         "sha256 client cannot fetch sha1",
			clientFormat: formatcfg.SHA256,
			serverTag:    ".git",
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := fixtures.ByTag(tc.serverTag).One()
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

// sha1OnlyStorage wraps a storage.Storer to hide any ObjectFormatGetter
// or ObjectFormatSetter implementations, simulating a storage backend
// that only supports SHA1.
type sha1OnlyStorage struct {
	storage.Storer
}

func TestFailSafeUnsupportedStorage(t *testing.T) {
	t.Parallel()

	t.Run("clone", func(t *testing.T) {
		t.Parallel()

		f := fixtures.ByTag(".git-sha256").One()
		require.NotNil(t, f, "fixture not found for tag .git-sha256")

		for _, srv := range server.All(server.Loader(t, f)) {
			endpoint, err := srv.Start()
			require.NoError(t, err)

			t.Cleanup(func() {
				require.NoError(t, srv.Close())
			})

			st := &sha1OnlyStorage{memory.NewStorage()}
			_, okGetter := storage.Storer(st).(xstorage.ObjectFormatGetter)
			assert.False(t, okGetter, "sha1OnlyStorage must not implement ObjectFormatGetter")

			_, okSetter := storage.Storer(st).(xstorage.ObjectFormatSetter)
			assert.False(t, okSetter, "sha1OnlyStorage must not implement ObjectFormatSetter")

			_, err = Clone(st, nil, &CloneOptions{URL: endpoint})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "mismatched algorithms")
		}
	})

	t.Run("open", func(t *testing.T) {
		t.Parallel()

		f := fixtures.ByTag(".git-sha256").One()
		require.NotNil(t, f, "fixture not found for tag .git-sha256")

		st := filesystem.NewStorage(f.DotGit(fixtures.WithMemFS()), cache.NewObjectLRUDefault())

		wrapped := &sha1OnlyStorage{st}
		_, okGetter := storage.Storer(wrapped).(xstorage.ObjectFormatGetter)
		assert.False(t, okGetter, "sha1OnlyStorage must not implement ObjectFormatGetter")

		_, okSetter := storage.Storer(wrapped).(xstorage.ObjectFormatSetter)
		assert.False(t, okSetter, "sha1OnlyStorage must not implement ObjectFormatSetter")

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
	s.ErrorIs(err, context.Canceled)
}

func (s *RepositorySuite) TestCloneMirror() {
	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{
		URL:    fixtures.Basic().One().URL,
		Mirror: true,
	})

	s.NoError(err)

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
	remote, err := r.CreateRemote(&config.RemoteConfig{})

	s.ErrorIs(err, config.ErrRemoteConfigEmptyName)
	s.Nil(remote)
}

func (s *RepositorySuite) TestCreateRemoteAnonymous() {
	r, _ := Init(memory.NewStorage())
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: "anonymous",
		URLs: []string{"http://foo/foo.git"},
	})

	s.NoError(err)
	s.Equal("anonymous", remote.Config().Name)
}

func (s *RepositorySuite) TestCreateRemoteAnonymousInvalidName() {
	r, _ := Init(memory.NewStorage())
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: "not_anonymous",
		URLs: []string{"http://foo/foo.git"},
	})

	s.ErrorIs(err, ErrAnonymousRemoteName)
	s.Nil(remote)
}

func (s *RepositorySuite) TestCreateRemoteAnonymousInvalid() {
	r, _ := Init(memory.NewStorage())
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{})

	s.ErrorIs(err, config.ErrRemoteConfigEmptyName)
	s.Nil(remote)
}

func (s *RepositorySuite) TestDeleteRemote() {
	r, _ := Init(memory.NewStorage())
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
	err := r.CreateBranch(&config.Branch{})

	s.NotNil(err)
}

func (s *RepositorySuite) TestInvalidCreateBranch() {
	r, _ := Init(memory.NewStorage())
	err := r.CreateBranch(&config.Branch{
		Name: "-foo",
	})

	s.NotNil(err)
}

func (s *RepositorySuite) TestCreateBranchAndBranch() {
	r, _ := Init(memory.NewStorage())
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
	branch, err := r.Branch("foo")

	s.NotNil(err)
	s.Nil(branch)
}

func (s *RepositorySuite) TestCreateBranchInvalid() {
	r, _ := Init(memory.NewStorage())
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

func (s *RepositorySuite) TestPlainInitAlreadyExists() {
	dir := s.T().TempDir()
	r, err := PlainInit(dir, true)
	s.NoError(err)
	s.NotNil(r)

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
	}
}

func (s *RepositorySuite) testPlainOpenGitFile(f func(string, string) string) {
	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "plain-open")
	s.NoError(err)

	r, err := PlainInit(fs.Join(fs.Root(), dir), true)
	s.NoError(err)
	s.NotNil(r)

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

	altDir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"),
		fmt.Appendf(nil, "gitdir: %s\nTRAILING", fs.Join(fs.Root(), altDir)),
		0o644,
	)
	s.NoError(err)

	r, err = PlainOpen(altDir)
	s.ErrorIs(err, ErrRepositoryNotExists)
	s.Nil(r)
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileBadPrefix() {
	fs := s.TemporalFilesystem()

	dir, err := util.TempDir(fs, "", "")
	s.NoError(err)

	r, err := PlainInit(fs.Join(fs.Root(), dir), true)
	s.NoError(err)
	s.NotNil(r)

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

	opt := &PlainOpenOptions{DetectDotGit: true}
	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), subdir), opt)
	s.NoError(err)
	s.NotNil(r)

	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), file), opt)
	s.NoError(err)
	s.NotNil(r)

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

	remote, err := r.Remote("test")
	s.NoError(err)
	s.NotNil(remote)
}

func (s *RepositorySuite) TestPlainCloneOverExistingGitDirectory() {
	dir := s.T().TempDir()
	r, err := PlainInit(dir, false)
	s.NotNil(r)
	s.NoError(err)

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

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:               s.GetLocalRepositoryURL(fixtures.ByTag("submodule").One()),
		RecurseSubmodules: DefaultSubmoduleRecursionDepth,
	})
	s.Require().NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Remotes, 1)
	s.Len(cfg.Branches, 1)
	s.Len(cfg.Submodules, 2)
}

func (s *RepositorySuite) TestPlainCloneWithShallowSubmodules() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	dir := s.T().TempDir()
	path := fixtures.ByTag("submodule").One().Worktree().Root()
	mainRepo, err := PlainClone(dir, &CloneOptions{
		URL:               path,
		RecurseSubmodules: 1,
		ShallowSubmodules: true,
	})
	s.Require().NoError(err)

	mainWorktree, err := mainRepo.Worktree()
	s.Require().NoError(err)

	submodule, err := mainWorktree.Submodule("basic")
	s.Require().NoError(err)

	subRepo, err := submodule.Repository()
	s.Require().NoError(err)

	lr, err := subRepo.Log(&LogOptions{})
	s.Require().NoError(err)

	commitCount := 0
	for _, err := lr.Next(); err == nil; _, err = lr.Next() {
		commitCount++
	}
	s.NoError(err)
	s.Equal(1, commitCount)
}

func (s *RepositorySuite) TestPlainCloneNoCheckout() {
	dir := s.T().TempDir()

	r, err := PlainClone(dir, &CloneOptions{
		URL:               s.GetLocalRepositoryURL(fixtures.ByTag("submodule").One()),
		NoCheckout:        true,
		RecurseSubmodules: DefaultSubmoduleRecursionDepth,
	})
	s.Require().NoError(err)

	h, err := r.Head()
	s.NoError(err)
	s.Equal("b685400c1f9316f350965a5993d350bc746b0bf4", h.Hash().String())

	fi, err := osfs.New(dir).ReadDir("")
	s.NoError(err)
	s.Len(fi, 1) // .git
}

func (s *RepositorySuite) TestFetch() {
	r, _ := Init(memory.NewStorage())
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
	_, err := Clone(memory.NewStorage(), fs, &CloneOptions{
		URL:      s.GetBasicLocalRepositoryURL(),
		Progress: buf,
	})

	s.NoError(err)
	s.NotEqual(0, buf.Len())
}

func (s *RepositorySuite) TestCloneDeep() {
	fs := memfs.New()
	r, _ := Init(memory.NewStorage(), WithWorkTree(fs))

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
	_, err := PlainInit(url, true)
	s.NoError(err)

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

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL:   server.wt.Root(),
		Depth: 1,
	})
	s.NoError(err)

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
	srcFs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	r, err := Open(sto, srcFs)
	s.NoError(err)

	err = r.Push(&PushOptions{RemoteName: "myremote"})
	s.ErrorContains(err, "remote not found")
}

func (s *RepositorySuite) TestLog() {
	r, _ := Init(memory.NewStorage())
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
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	ref, err := r.CreateTag("foobar", plumbing.ZeroHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	s.Nil(ref)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestCreateTagSigned() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	h, err := r.Head()
	s.NoError(err)

	key := commitSignKey(s.T(), true)
	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
		SignKey: key,
	})
	s.NoError(err)

	tag, err := r.Tag("foobar")
	s.NoError(err)

	obj, err := r.TagObject(tag.Hash())
	s.NoError(err)

	// Verify the tag.
	pks := new(bytes.Buffer)
	pkw, err := armor.Encode(pks, openpgp.PublicKeyType, nil)
	s.NoError(err)

	err = key.Serialize(pkw)
	s.NoError(err)
	err = pkw.Close()
	s.NoError(err)

	actual, err := obj.Verify(pks.String())
	s.NoError(err)
	s.Equal(key.PrimaryKey, actual.PrimaryKey)
}

func (s *RepositorySuite) TestCreateTagSignedBadKey() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	h, err := r.Head()
	s.NoError(err)

	key := commitSignKey(s.T(), false)
	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
		SignKey: key,
	})
	s.ErrorIs(err, openpgperr.InvalidArgumentError("signing key is encrypted"))
}

func (s *RepositorySuite) TestCreateTagCanonicalize() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	s.NoError(err)

	h, err := r.Head()
	s.NoError(err)

	key := commitSignKey(s.T(), true)
	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "\n\nfoo bar baz qux\n\nsome message here",
		SignKey: key,
	})
	s.NoError(err)

	tag, err := r.Tag("foobar")
	s.NoError(err)

	obj, err := r.TagObject(tag.Hash())
	s.NoError(err)

	// Assert the new canonicalized message.
	s.Equal("foo bar baz qux\n\nsome message here\n", obj.Message)

	// Verify the tag.
	pks := new(bytes.Buffer)
	pkw, err := armor.Encode(pks, openpgp.PublicKeyType, nil)
	s.NoError(err)

	err = key.Serialize(pkw)
	s.NoError(err)
	err = pkw.Close()
	s.NoError(err)

	actual, err := obj.Verify(pks.String())
	s.NoError(err)
	s.Equal(key.PrimaryKey, actual.PrimaryKey)
}

func (s *RepositorySuite) TestTagLightweight() {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage())
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

	r, err = PlainOpen(fs.Root())
	s.NotNil(r)
	s.NoError(err)

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
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	r, err := Open(sto, f.DotGit())
	s.NoError(err)

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
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	commits, err := r.CommitObjects()
	s.NoError(err)
	commits.Close()
}

func (s *RepositorySuite) TestRef() {
	r, _ := Init(memory.NewStorage())
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
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.NoError(err)

	s.NoError(err)

	iter, err := r.References()
	s.NoError(err)
	s.NotNil(iter)
}

func (s *RepositorySuite) TestObject() {
	r, _ := Init(memory.NewStorage())
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
	w, err := r.Worktree()
	s.NoError(err)
	s.Equal(def, w.Filesystem)
}

func (s *RepositorySuite) TestWorktreeBare() {
	r, _ := Init(memory.NewStorage())
	w, err := r.Worktree()
	s.ErrorIs(err, ErrIsBareRepository)
	s.Nil(w)
}

func (s *RepositorySuite) TestResolveRevision() {
	f := fixtures.ByURL("https://github.com/git-fixtures/basic.git").One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	r, err := Open(sto, f.DotGit())
	s.NoError(err)

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
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	r, err := Open(sto, f.DotGit())
	s.NoError(err)

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
	srcFs := fixtures.ByTag("unpacked").One().DotGit()
	var sto storage.Storer
	var err error
	sto = filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

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
			fs := f.DotGit()
			st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

			worktree, err := fs.Chroot(filepath.Dir(fs.Root()))
			if err != nil {
				b.Fatal(err)
			}

			repo, err := Open(st, worktree)
			if err != nil {
				b.Fatal(err)
			}

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
		_, err := PlainClone(b.TempDir(), &CloneOptions{
			URL:          "https://github.com/go-git/go-git.git",
			Depth:        1,
			Tags:         plumbing.NoTags,
			SingleBranch: true,
			Bare:         true,
		})
		if err != nil {
			b.Error(err)
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
