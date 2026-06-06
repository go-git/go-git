package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
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
	"golang.org/x/text/unicode/norm"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func defaultTestCommitOptions() *CommitOptions {
	return &CommitOptions{
		Author: &object.Signature{Name: "testuser", Email: "testemail"},
	}
}

type WorktreeSuite struct {
	BaseSuite
}

func TestWorktreeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(WorktreeSuite))
}

func (s *WorktreeSuite) SetupSuite() {
	// WorktreeSuite creates its own repository in SetupTest,
	// so we don't call buildBasicRepository() here to avoid creating
	// a repository that will be immediately overwritten.
	// We only initialize the cache.
	s.cache = make(map[string]*Repository)
}

func (s *WorktreeSuite) SetupTest() {
	f := fixtures.Basic().One()
	r := NewRepositoryWithEmptyWorktree(f)
	s.T().Cleanup(func() {
		_ = r.Close()
	})
	s.Repository = r
}

func (s *WorktreeSuite) TestPullCheckout() {
	fs := memfs.New()
	r, _ := Init(memory.NewStorage(), WithWorkTree(fs))
	defer func() { _ = r.Close() }()
	r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	w, err := r.Worktree()
	s.NoError(err)

	err = w.Pull(&PullOptions{})
	s.NoError(err)

	fi, err := fs.ReadDir("")
	s.NoError(err)
	s.Len(fi, 8)
}

func (s *WorktreeSuite) TestPullFastForward() {
	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: url})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{URL: server.wt.Root()})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	w, err := server.Worktree()
	s.NoError(err)
	s.NoError(util.WriteFile(w.filesystem, "foo", []byte("foo"), 0o755))
	w.Add("foo")
	hash, err := w.Commit("foo", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	w, err = r.Worktree()
	s.NoError(err)

	err = w.Pull(&PullOptions{})
	s.NoError(err)

	head, err := r.Head()
	s.NoError(err)
	s.Equal(hash, head.Hash())
}

func (s *WorktreeSuite) TestPullNonFastForward() {
	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: url})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{URL: server.wt.Root()})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	w, err := server.Worktree()
	s.NoError(err)
	s.NoError(util.WriteFile(w.filesystem, "foo", []byte("foo"), 0o755))
	w.Add("foo")
	_, err = w.Commit("foo", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	w, err = r.Worktree()
	s.NoError(err)
	s.NoError(util.WriteFile(w.filesystem, "bar", []byte("bar"), 0o755))
	w.Add("bar")
	_, err = w.Commit("bar", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	err = w.Pull(&PullOptions{})
	s.ErrorIs(err, ErrNonFastForwardUpdate)
}

func (s *WorktreeSuite) TestPullUpdateReferencesIfNeeded() {
	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()
	r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err := r.Fetch(&FetchOptions{})
	s.NoError(err)

	_, err = r.Reference("refs/heads/master", false)
	s.NotNil(err)

	w, err := r.Worktree()
	s.NoError(err)

	err = w.Pull(&PullOptions{})
	s.NoError(err)

	head, err := r.Reference(plumbing.HEAD, true)
	s.NoError(err)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", head.Hash().String())

	branch, err := r.Reference("refs/heads/master", false)
	s.NoError(err)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())

	err = w.Pull(&PullOptions{})
	s.ErrorIs(err, NoErrAlreadyUpToDate)
}

func (s *WorktreeSuite) TestPullInSingleBranch() {
	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()
	err := r.clone(context.Background(), &CloneOptions{
		URL:          s.GetBasicLocalRepositoryURL(),
		SingleBranch: true,
	})

	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	err = w.Pull(&PullOptions{})
	s.ErrorIs(err, NoErrAlreadyUpToDate)

	branch, err := r.Reference("refs/heads/master", false)
	s.NoError(err)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())

	_, err = r.Reference("refs/remotes/foo/branch", false)
	s.NotNil(err)

	storage := r.Storer.(*memory.Storage)
	s.Len(storage.Objects, 28)
}

func (s *WorktreeSuite) TestPullProgress() {
	s.T().Skip("skipping test: progress capability not supported in server-side transport. " +
		"Local repositories use both server and client transport implementations. " +
		"See upload-pack.go for details.")

	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()

	r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	w, err := r.Worktree()
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = w.Pull(&PullOptions{
		Progress: buf,
	})

	s.NoError(err)
	s.NotEqual(0, buf.Len())
}

func (s *WorktreeSuite) TestPullProgressWithRecursion() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	path := s.GetLocalRepositoryURL(fixtures.ByTag("submodule").One())

	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()
	r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{path},
	})

	w, err := r.Worktree()
	s.Require().NoError(err)

	err = w.Pull(&PullOptions{
		RecurseSubmodules: DefaultSubmoduleRecursionDepth,
	})
	s.Require().NoError(err)

	cfg, err := r.Config()
	s.NoError(err)
	s.Len(cfg.Submodules, 2)
}

func (s *RepositorySuite) TestPullAdd() {
	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: url})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{URL: server.wt.Root()})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	storage := r.Storer.(*memory.Storage)
	s.Len(storage.Objects, 28)

	branch, err := r.Reference("refs/heads/master", false)
	s.NoError(err)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())

	ExecuteOnPath(s.T(), server.wt.Root(),
		"touch foo",
		"git add foo",
		"git commit --no-gpg-sign -m foo foo",
	)

	w, err := r.Worktree()
	s.NoError(err)

	err = w.Pull(&PullOptions{RemoteName: "origin"})
	s.NoError(err)

	// the commit command has introduced a new commit, tree and blob
	s.Len(storage.Objects, 31)

	branch, err = r.Reference("refs/heads/master", false)
	s.NoError(err)
	s.NotEqual("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branch.Hash().String())
}

func (s *WorktreeSuite) TestPullAlreadyUptodate() {
	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	fs := memfs.New()
	r, err := Clone(memory.NewStorage(), fs, &CloneOptions{URL: url})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)
	err = util.WriteFile(fs, "bar", []byte("bar"), 0o755)
	s.NoError(err)
	w.Add("bar")
	_, err = w.Commit("bar", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	err = w.Pull(&PullOptions{})
	s.ErrorIs(err, NoErrAlreadyUpToDate)
}

func (s *WorktreeSuite) TestPullDepth() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")
	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL:   s.GetBasicLocalRepositoryURL(),
		Depth: 1,
	})

	s.NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)
	err = w.Pull(&PullOptions{})
	s.NoError(err)
}

func (s *WorktreeSuite) TestPullAfterShallowClone() {
	tempDir := s.T().TempDir()
	remoteURL := filepath.Join(tempDir, "remote")
	repoDir := filepath.Join(tempDir, "repo")

	remote, err := PlainInit(remoteURL, false)
	s.NoError(err)
	defer func() { _ = remote.Close() }()
	s.NotNil(remote)

	_ = CommitNewFile(s.T(), remote, "File1")
	_ = CommitNewFile(s.T(), remote, "File2")

	repo, err := PlainClone(repoDir, &CloneOptions{
		URL:           remoteURL,
		Depth:         1,
		Tags:          plumbing.NoTags,
		SingleBranch:  true,
		ReferenceName: "master",
	})
	s.NoError(err)
	defer func() { _ = repo.Close() }()

	_ = CommitNewFile(s.T(), remote, "File3")
	_ = CommitNewFile(s.T(), remote, "File4")

	w, err := repo.Worktree()
	s.NoError(err)

	err = w.Pull(&PullOptions{
		RemoteName:    DefaultRemoteName,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName("master"),
	})
	s.NoError(err)
}

// TestPullAfterShallowClone_UntrackedFilesPreserved checks that an untracked
// file in a shallow-cloned worktree is not deleted by a subsequent pull.
// Regression test for https://github.com/go-git/go-git/issues/1719.
func (s *WorktreeSuite) TestPullAfterShallowClone_UntrackedFilesPreserved() {
	tempDir := s.T().TempDir()
	remoteURL := filepath.Join(tempDir, "remote")
	repoDir := filepath.Join(tempDir, "repo")

	remote, err := PlainInit(remoteURL, false)
	s.Require().NoError(err)
	defer func() { _ = remote.Close() }()

	_ = CommitNewFile(s.T(), remote, "File1")
	_ = CommitNewFile(s.T(), remote, "File2")

	repo, err := PlainClone(repoDir, &CloneOptions{
		URL:           remoteURL,
		Depth:         1,
		Tags:          plumbing.NoTags,
		SingleBranch:  true,
		ReferenceName: "master",
	})
	s.Require().NoError(err)
	defer func() { _ = repo.Close() }()

	// Create an untracked file in the cloned worktree.
	untrackedPath := filepath.Join(repoDir, "untracked.txt")
	s.Require().NoError(os.WriteFile(untrackedPath, []byte("do not delete me"), 0o644))

	// Push new commits to the remote so the pull has actual work to do.
	_ = CommitNewFile(s.T(), remote, "File3")
	_ = CommitNewFile(s.T(), remote, "File4")

	w, err := repo.Worktree()
	s.Require().NoError(err)

	// Test without Force first.
	err = w.Pull(&PullOptions{
		RemoteName:    DefaultRemoteName,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName("master"),
	})
	s.Require().NoError(err)

	// The untracked file must survive the pull (mirrors real git behaviour).
	_, statErr := os.Stat(untrackedPath)
	s.Require().NoError(statErr, "untracked file was removed by pull (no Force) on a shallow clone")

	// Push another commit and test with Force: true.
	_ = CommitNewFile(s.T(), remote, "File5")

	err = w.Pull(&PullOptions{
		RemoteName:    DefaultRemoteName,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName("master"),
		Force:         true,
	})
	s.Require().NoError(err)

	_, statErr = os.Stat(untrackedPath)
	s.Require().NoError(statErr, "untracked file was removed by pull (Force: true) on a shallow clone")
}

func (s *WorktreeSuite) TestCheckout() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Force: true,
	})
	s.NoError(err)

	entries, err := fs.ReadDir("/")
	s.NoError(err)

	s.Len(entries, 8)
	ch, err := fs.Open("CHANGELOG")
	s.NoError(err)

	content, err := io.ReadAll(ch)
	s.NoError(err)
	s.Equal("Initial changelog\n", string(content))

	idx, err := s.Repository.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)
}

func (s *WorktreeSuite) TestCheckoutForce() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	w.filesystem = newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS())

	err = w.Checkout(&CheckoutOptions{
		Force: true,
	})
	s.NoError(err)

	entries, err := w.filesystem.ReadDir("/")
	s.NoError(err)
	s.Len(entries, 8)
}

func (s *WorktreeSuite) TestCheckoutKeep() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Force: true,
	})
	s.NoError(err)

	// Create a new branch and create a new file.
	err = w.Checkout(&CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("new-branch"),
		Create: true,
	})
	s.NoError(err)

	w.filesystem = newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS())
	f, err := w.filesystem.Create("new-file.txt")
	s.NoError(err)
	_, err = f.Write([]byte("DUMMY"))
	s.NoError(err)
	s.Nil(f.Close())

	// Add the file to staging.
	_, err = w.Add("new-file.txt")
	s.NoError(err)

	// Switch branch to master, and verify that the new file was kept in staging.
	err = w.Checkout(&CheckoutOptions{
		Keep: true,
	})
	s.NoError(err)

	fi, err := w.filesystem.Stat("new-file.txt")
	s.NoError(err)
	s.Equal(int64(5), fi.Size())
}

func (s *WorktreeSuite) TestCheckoutSymlink() {
	if runtime.GOOS == "windows" {
		s.T().Skip("git doesn't support symlinks by default in windows")
	}

	dir := s.T().TempDir()

	r, err := PlainInit(dir, false)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)

	w.filesystem.Symlink("not-exists", "bar")
	w.Add("bar")
	w.Commit("foo", &CommitOptions{Author: defaultSignature()})

	r.Storer.SetIndex(&index.Index{Version: 2})
	w.filesystem = newWorktreeFilesystem(osfs.New(filepath.Join(dir, "worktree-empty")), defaultProtectNTFS(), defaultProtectHFS())

	err = w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())

	target, err := w.filesystem.Readlink("bar")
	s.Equal("not-exists", target)
	s.NoError(err)
}

func TestCheckoutSymlinkArbitraryTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git doesn't support symlinks by default in windows")
	}
	t.Parallel()

	tests := []struct {
		name   string
		target string
	}{
		{name: "rel", target: "target"},
		{name: "absolute", target: "/etc/passwd"},
		{name: "dot-dot relative", target: "../../outside"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			r, err := PlainInit(dir, false)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			w, err := r.Worktree()
			require.NoError(t, err)

			require.NoError(t, w.filesystem.Symlink(tc.target, "link"))
			_, err = w.Add("link")
			require.NoError(t, err)
			_, err = w.Commit("add symlink", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)

			require.NoError(t, r.Storer.SetIndex(&index.Index{Version: 2}))
			w.filesystem = newWorktreeFilesystem(
				osfs.New(filepath.Join(dir, "worktree-empty")), true, true)

			require.NoError(t, w.Checkout(&CheckoutOptions{}))

			status, err := w.Status()
			require.NoError(t, err)
			assert.True(t, status.IsClean())

			got, err := w.filesystem.Readlink("link")
			require.NoError(t, err)
			assert.Equal(t, tc.target, got)
		})
	}
}

func (s *WorktreeSuite) TestCheckoutSparse() {
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
		SparseCheckoutDirectories: sparseCheckoutDirectories,
	}))

	fis, err := fs.ReadDir("/")
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

func (s *WorktreeSuite) TestCheckoutCRLF() {
	runTest := func(t *testing.T, autoCRLF string) (result []byte) {
		r := NewRepositoryWithEmptyWorktree(fixtures.Basic().One())
		defer func() { _ = r.Close() }()

		cfg, err := r.Config()
		require.NoError(t, err)
		cfg.Core.AutoCRLF = autoCRLF
		require.NoError(t, r.SetConfig(cfg))

		wt, err := r.Worktree()
		require.NoError(t, err)

		require.NoError(t, wt.Checkout(&CheckoutOptions{}))

		status, err := wt.Status()
		require.NoError(t, err)
		require.True(t, status.IsClean(), status)

		file, err := wt.filesystem.Open("CHANGELOG")
		require.NoError(t, err)

		content, err := io.ReadAll(file)
		require.NoError(t, err)

		return content
	}

	s.Run("autocrlf=true", func() {
		result := runTest(s.T(), "true")
		s.Equal("Initial changelog\r\n", string(result))
	})

	s.Run("autocrlf=input", func() {
		result := runTest(s.T(), "input")
		s.Equal("Initial changelog\n", string(result))
	})
}

func (s *WorktreeSuite) TestFilenameNormalization() {
	if runtime.GOOS == "windows" {
		s.T().Skip("windows paths may contain non utf-8 sequences")
	}

	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: url})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	filename := "페"

	w, err := server.Worktree()
	s.Require().NoError(err)

	writeFile := func(fs billy.Filesystem, path string) {
		s.Require().NoError(util.WriteFile(fs, path, []byte("foo"), 0o755))
	}

	writeFile(w.filesystem, filename)
	origHash, err := w.Add(filename)
	s.NoError(err)
	_, err = w.Commit("foo", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{URL: server.wt.Root()})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	w, err = r.Worktree()
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())

	err = w.filesystem.Remove(filename)
	s.NoError(err)

	modFilename := norm.NFKD.String(filename)
	writeFile(w.filesystem, modFilename)

	_, err = w.Add(filename)
	s.NoError(err)
	modHash, err := w.Add(modFilename)
	s.NoError(err)
	// At this point we've got two files with the same content.
	// Hence their hashes must be the same.
	s.True(origHash == modHash)

	status, err = w.Status()
	s.NoError(err)
	// However, their names are different and the work tree is still dirty.
	s.False(status.IsClean())

	// Revert back the deletion of the first file.
	writeFile(w.filesystem, filename)
	_, err = w.Add(filename)
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	// Still dirty - the second file is added.
	s.False(status.IsClean())

	_, err = w.Remove(modFilename)
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutSubmodule() {
	url := "https://github.com/git-fixtures/submodule.git"
	r := NewRepositoryWithEmptyWorktree(fixtures.ByURL(url).One())
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)

	err = w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutSubmoduleInitialized() {
	url := "https://github.com/git-fixtures/submodule.git"
	r := s.NewRepository(fixtures.ByURL(url).One())
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)

	sub, err := w.Submodules()
	s.NoError(err)

	err = sub.Update(&SubmoduleUpdateOptions{Init: true})
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutRelativePathSubmoduleInitialized() {
	url := "https://github.com/git-fixtures/submodule.git"
	r := s.NewRepository(fixtures.ByURL(url).One())
	defer func() { _ = r.Close() }()

	// modify the .gitmodules from original one
	file, err := r.wt.OpenFile(".gitmodules", os.O_WRONLY|os.O_TRUNC, 0o666)
	s.NoError(err)

	n, err := io.WriteString(file, `[submodule "basic"]
	path = basic
	url = ../basic.git
[submodule "itself"]
	path = itself
	url = ../submodule.git`)
	s.NoError(err)
	s.NotEqual(0, n)

	w, err := r.Worktree()
	s.NoError(err)

	w.Add(".gitmodules")
	w.Commit("test", &CommitOptions{Author: defaultSignature()})

	// test submodule path
	modules, err := w.readGitmodulesFile()
	s.NoError(err)

	s.Equal("../basic.git", modules.Submodules["basic"].URL)
	s.Equal("../submodule.git", modules.Submodules["itself"].URL)

	basicSubmodule, err := w.Submodule("basic")
	s.NoError(err)
	basicRepo, err := basicSubmodule.Repository()
	s.NoError(err)
	defer basicRepo.Close()
	basicRemotes, err := basicRepo.Remotes()
	s.NoError(err)
	s.Equal("https://github.com/git-fixtures/basic.git", basicRemotes[0].Config().URLs[0])

	itselfSubmodule, err := w.Submodule("itself")
	s.NoError(err)
	itselfRepo, err := itselfSubmodule.Repository()
	s.NoError(err)
	defer itselfRepo.Close()
	itselfRemotes, err := itselfRepo.Remotes()
	s.NoError(err)
	s.Equal("https://github.com/git-fixtures/submodule.git", itselfRemotes[0].Config().URLs[0])

	sub, err := w.Submodules()
	s.NoError(err)

	err = sub.Update(&SubmoduleUpdateOptions{Init: true, RecurseSubmodules: DefaultSubmoduleRecursionDepth})
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutIndexMem() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	idx, err := s.Repository.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)
	s.Equal("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", idx.Entries[0].Hash.String())
	s.Equal(".gitignore", idx.Entries[0].Name)
	s.Equal(filemode.Regular, idx.Entries[0].Mode)
	s.False(idx.Entries[0].ModifiedAt.IsZero())
	s.Equal(uint32(189), idx.Entries[0].Size)

	// ctime, dev, inode, uid and gid are not supported on memfs fs
	s.True(idx.Entries[0].CreatedAt.IsZero())
	s.Equal(uint32(0), idx.Entries[0].Dev)
	s.Equal(uint32(0), idx.Entries[0].Inode)
	s.Equal(uint32(0), idx.Entries[0].UID)
	s.Equal(uint32(0), idx.Entries[0].GID)
}

func (s *WorktreeSuite) TestCheckoutIndexOS() {
	fs := s.TemporalFilesystem()

	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	idx, err := s.Repository.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)
	s.Equal("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", idx.Entries[0].Hash.String())
	s.Equal(".gitignore", idx.Entries[0].Name)
	s.Equal(filemode.Regular, idx.Entries[0].Mode)
	s.False(idx.Entries[0].ModifiedAt.IsZero())
	s.Equal(uint32(189), idx.Entries[0].Size)

	s.False(idx.Entries[0].CreatedAt.IsZero())
	if runtime.GOOS != "windows" {
		s.NotEqual(uint32(0), idx.Entries[0].Dev)
		s.NotEqual(uint32(0), idx.Entries[0].Inode)
		s.NotEqual(uint32(0), idx.Entries[0].UID)
		s.NotEqual(uint32(0), idx.Entries[0].GID)
	}
}

func (s *WorktreeSuite) TestCheckoutBranch() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Branch: "refs/heads/branch",
	})
	s.NoError(err)

	head, err := w.r.Head()
	s.NoError(err)
	s.Equal("refs/heads/branch", head.Name().String())

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutBranchUntracked() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	uf, err := w.filesystem.Create("untracked_file")
	s.NoError(err)
	_, err = uf.Write([]byte("don't delete me"))
	s.NoError(err)

	err = w.Checkout(&CheckoutOptions{
		Branch: "refs/heads/branch",
	})
	s.NoError(err)

	head, err := w.r.Head()
	s.NoError(err)
	s.Equal("refs/heads/branch", head.Name().String())

	status, err := w.Status()
	s.NoError(err)
	// The untracked file should still be there, so it's not clean
	s.False(status.IsClean())
	s.True(status.IsUntracked("untracked_file"))
	err = w.filesystem.Remove("untracked_file")
	s.NoError(err)
	status, err = w.Status()
	s.NoError(err)
	// After deleting the untracked file it should now be clean
	s.True(status.IsClean())
}

// TestCheckoutForceUntracked verifies that Checkout with Force:true (HardReset)
// does not delete untracked files, matching real git checkout -f behaviour.
func (s *WorktreeSuite) TestCheckoutForceUntracked() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	// Populate the worktree first.
	err := w.Checkout(&CheckoutOptions{})
	s.Require().NoError(err)

	// Create an untracked file.
	uf, err := w.filesystem.Create("untracked_file")
	s.Require().NoError(err)
	_, err = uf.Write([]byte("don't delete me"))
	s.Require().NoError(err)
	s.Require().NoError(uf.Close())

	// Force checkout (HardReset) — should leave untracked files alone.
	err = w.Checkout(&CheckoutOptions{Force: true})
	s.Require().NoError(err)

	status, err := w.Status()
	s.Require().NoError(err)
	// Untracked file must still be present.
	s.True(status.IsUntracked("untracked_file"), "untracked file was deleted by Force checkout")
}

// TestCheckoutForceSparseUntrackedPreserved verifies that when switching
// SparseCheckoutDirectories with Force:true, tracked files outside the new
// sparse set are removed from disk but untracked files inside those directories
// are preserved — matching real git behaviour (git keeps the untracked file
// and prints a warning).
func (s *WorktreeSuite) TestCheckoutForceSparseUntrackedPreserved() {
	fs := memfs.New()
	r, err := Clone(memory.NewStorage(), fs, &CloneOptions{
		URL:        s.GetBasicLocalRepositoryURL(),
		NoCheckout: true,
	})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.Require().NoError(err)

	// Initial sparse checkout: only the "go" directory.
	s.Require().NoError(w.Checkout(&CheckoutOptions{
		SparseCheckoutDirectories: []string{"go"},
	}))

	// Confirm that at least one tracked file is present inside "go/".
	goFiles, err := fs.ReadDir("go")
	s.Require().NoError(err)
	s.Require().NotEmpty(goFiles, "expected tracked files in go/ after sparse checkout")

	// Create an untracked file inside the soon-to-be-removed sparse directory.
	uf, err := w.filesystem.Create("go/untracked.txt")
	s.Require().NoError(err)
	_, err = uf.Write([]byte("do not delete me"))
	s.Require().NoError(err)
	s.Require().NoError(uf.Close())

	// Force checkout: switch sparse set from "go" to "php".
	// Real git removes tracked "go/" files from the worktree but preserves
	// untracked files (showing a warning). go-git must match this behaviour.
	s.Require().NoError(w.Checkout(&CheckoutOptions{
		Force:                     true,
		SparseCheckoutDirectories: []string{"php"},
	}))

	// Untracked file inside the removed sparse directory must be preserved.
	_, statErr := w.filesystem.Stat("go/untracked.txt")
	s.Require().NoError(statErr, "untracked file inside removed sparse dir was deleted by Force checkout")

	// Tracked files that moved out of the sparse set must be removed from disk.
	firstGoFile := filepath.Join("go", goFiles[0].Name())
	_, statErr = w.filesystem.Stat(firstGoFile)
	s.Require().Error(statErr, "tracked file %q in removed sparse dir should have been removed by Force checkout", firstGoFile)

	// The "php" directory must now be present and populated.
	phpFiles, err := fs.ReadDir("php")
	s.Require().NoError(err)
	s.NotEmpty(phpFiles, "expected php/ files after sparse checkout switch")
}

func (s *WorktreeSuite) TestCheckoutCreateWithHash() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Create: true,
		Branch: "refs/heads/foo",
		Hash:   plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	s.NoError(err)

	head, err := w.r.Head()
	s.NoError(err)
	s.Equal("refs/heads/foo", head.Name().String())
	s.Equal(plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"), head.Hash())

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutCreate() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Create: true,
		Branch: "refs/heads/foo",
	})
	s.NoError(err)

	head, err := w.r.Head()
	s.NoError(err)
	s.Equal("refs/heads/foo", head.Name().String())
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), head.Hash())

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestCheckoutBranchAndHash() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Branch: "refs/heads/foo",
		Hash:   plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})

	s.ErrorIs(err, ErrBranchHashExclusive)
}

func (s *WorktreeSuite) TestCheckoutCreateMissingBranch() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		Create: true,
	})

	s.ErrorIs(err, ErrCreateRequiresBranch)
}

func (s *WorktreeSuite) TestCheckoutCreateInvalidBranch() {
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()),
	}

	for _, name := range []plumbing.ReferenceName{
		"foo",
		"-",
		"-foo",
		"refs/heads//",
		"refs/heads/..",
		"refs/heads/a..b",
		"refs/heads/.",
	} {
		err := w.Checkout(&CheckoutOptions{
			Create: true,
			Branch: name,
		})

		s.ErrorIs(err, plumbing.ErrInvalidReferenceName)
	}
}

func (s *WorktreeSuite) TestCheckoutTag() {
	f := fixtures.ByTag("tags").One()
	r := NewRepositoryWithEmptyWorktree(f)
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.NoError(err)

	err = w.Checkout(&CheckoutOptions{})
	s.NoError(err)
	head, err := w.r.Head()
	s.NoError(err)
	s.Equal("refs/heads/master", head.Name().String())

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())

	err = w.Checkout(&CheckoutOptions{Branch: "refs/tags/lightweight-tag"})
	s.NoError(err)
	head, err = w.r.Head()
	s.NoError(err)
	s.Equal("HEAD", head.Name().String())
	s.Equal("f7b877701fbf855b44c0a9e86f3fdce2c298b07f", head.Hash().String())

	err = w.Checkout(&CheckoutOptions{Branch: "refs/tags/commit-tag"})
	s.NoError(err)
	head, err = w.r.Head()
	s.NoError(err)
	s.Equal("HEAD", head.Name().String())
	s.Equal("f7b877701fbf855b44c0a9e86f3fdce2c298b07f", head.Hash().String())

	err = w.Checkout(&CheckoutOptions{Branch: "refs/tags/tree-tag"})
	s.NotNil(err)
	head, err = w.r.Head()
	s.NoError(err)
	s.Equal("HEAD", head.Name().String())
}

func (s *WorktreeSuite) TestCheckoutTagHash() {
	f := fixtures.ByTag("tags").One()
	r := NewRepositoryWithEmptyWorktree(f)
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.NoError(err)

	for _, hash := range []string{
		"b742a2a9fa0afcfa9a6fad080980fbc26b007c69", // annotated tag
		"ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc", // commit tag
		"f7b877701fbf855b44c0a9e86f3fdce2c298b07f", // lightweight tag
	} {
		err = w.Checkout(&CheckoutOptions{
			Hash: plumbing.NewHash(hash),
		})
		s.NoError(err)
		head, err := w.r.Head()
		s.NoError(err)
		s.Equal("HEAD", head.Name().String())

		status, err := w.Status()
		s.NoError(err)
		s.True(status.IsClean())
	}

	for _, hash := range []string{
		"fe6cb94756faa81e5ed9240f9191b833db5f40ae", // blob tag
		"152175bf7e5580299fa1f0ba41ef6474cc043b70", // tree tag
	} {
		err = w.Checkout(&CheckoutOptions{
			Hash: plumbing.NewHash(hash),
		})
		s.NotNil(err)
	}
}

func (s *WorktreeSuite) TestCheckoutBisect() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	s.testCheckoutBisect("https://github.com/src-d/go-git.git")
}

func (s *WorktreeSuite) TestCheckoutBisectSubmodules() {
	s.testCheckoutBisect("https://github.com/git-fixtures/submodule.git")
}

// TestCheckoutBisect simulates a git bisect going through the git history and
// checking every commit over the previous commit
func (s *WorktreeSuite) testCheckoutBisect(url string) {
	f := fixtures.ByURL(url).One()
	r := NewRepositoryWithEmptyWorktree(f)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)

	iter, err := w.r.Log(&LogOptions{})
	s.NoError(err)

	iter.ForEach(func(commit *object.Commit) error {
		err := w.Checkout(&CheckoutOptions{Hash: commit.Hash})
		s.NoError(err)

		status, err := w.Status()
		s.NoError(err)
		s.True(status.IsClean())

		return nil
	})
}

func (s *WorktreeSuite) TestStatus() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	status, err := w.Status()
	s.NoError(err)

	s.False(status.IsClean())
	s.Len(status, 9)
}

func (s *WorktreeSuite) TestStatusEmpty() {
	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	defer func() { _ = r.Close() }()
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
	s.NotNil(status)
}

func (s *WorktreeSuite) TestStatusCheckedInBeforeIgnored() {
	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	defer func() { _ = r.Close() }()
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	err = util.WriteFile(fs, "fileToIgnore", []byte("Initial data"), 0o755)
	s.NoError(err)
	_, err = w.Add("fileToIgnore")
	s.NoError(err)

	_, err = w.Commit("Added file that will be ignored later", defaultTestCommitOptions())
	s.NoError(err)

	err = util.WriteFile(fs, ".gitignore", []byte("fileToIgnore\nsecondIgnoredFile"), 0o755)
	s.NoError(err)
	_, err = w.Add(".gitignore")
	s.NoError(err)
	_, err = w.Commit("Added .gitignore", defaultTestCommitOptions())
	s.NoError(err)
	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
	s.NotNil(status)

	err = util.WriteFile(fs, "secondIgnoredFile", []byte("Should be completely ignored"), 0o755)
	s.NoError(err)
	status = nil
	status, err = w.Status()
	s.NoError(err)
	s.True(status.IsClean())
	s.NotNil(status)

	err = util.WriteFile(fs, "fileToIgnore", []byte("Updated data"), 0o755)
	s.NoError(err)
	status = nil
	status, err = w.Status()
	s.NoError(err)
	s.False(status.IsClean())
	s.NotNil(status)
}

func (s *WorktreeSuite) TestStatusEmptyDirty() {
	fs := memfs.New()
	err := util.WriteFile(fs, "foo", []byte("foo"), 0o755)
	s.NoError(err)

	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	defer func() { _ = r.Close() }()
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())
	s.Len(status, 1)
}

func (s *WorktreeSuite) TestStatusUnmodified() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	status, err := w.StatusWithOptions(StatusOptions{Strategy: Preload})
	s.NoError(err)
	s.True(status.IsClean())
	s.False(status.IsUntracked("LICENSE"))

	s.Equal(Unmodified, status.File("LICENSE").Staging)
	s.Equal(Unmodified, status.File("LICENSE").Worktree)

	status, err = w.StatusWithOptions(StatusOptions{Strategy: Empty})
	s.NoError(err)
	s.True(status.IsClean())
	s.False(status.IsUntracked("LICENSE"))

	s.Equal(Untracked, status.File("LICENSE").Staging)
	s.Equal(Untracked, status.File("LICENSE").Worktree)
}

func (s *WorktreeSuite) TestReset() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	branch, err := w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.NotEqual(commit, branch.Hash())

	err = w.Reset(&ResetOptions{Mode: MergeReset, Commit: commit})
	s.NoError(err)

	branch, err = w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commit, branch.Hash())

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestResetWithUntracked() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = util.WriteFile(fs, "foo", nil, 0o755)
	s.NoError(err)

	err = w.Reset(&ResetOptions{Mode: MergeReset, Commit: commit})
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	for file, st := range status {
		if file == "foo" {
			s.Equal(Untracked, st.Worktree)
			s.Equal(Untracked, st.Staging)
			continue
		}
		if st.Worktree != Unmodified || st.Staging != Unmodified {
			s.Fail("file %s not unmodified", file)
		}
	}
}

func (s *WorktreeSuite) TestResetSoft() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = w.Reset(&ResetOptions{Mode: SoftReset, Commit: commit})
	s.NoError(err)

	branch, err := w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commit, branch.Hash())

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())
	s.Equal(Added, status.File("CHANGELOG").Staging)
}

func (s *WorktreeSuite) TestResetMixed() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = w.Reset(&ResetOptions{Mode: MixedReset, Commit: commit})
	s.NoError(err)

	branch, err := w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commit, branch.Hash())

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())
	s.Equal(Untracked, status.File("CHANGELOG").Staging)
}

func (s *WorktreeSuite) TestResetMerge() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commitA := plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294")
	commitB := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = w.Reset(&ResetOptions{Mode: MergeReset, Commit: commitA})
	s.NoError(err)

	branch, err := w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commitA, branch.Hash())

	f, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("foo"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	err = w.Reset(&ResetOptions{Mode: MergeReset, Commit: commitB})
	s.ErrorIs(err, ErrUnstagedChanges)

	branch, err = w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commitA, branch.Hash())
}

func (s *WorktreeSuite) TestResetKeep() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.Require().NoError(err)

	// KeepReset with no local changes should succeed and update the worktree.
	err = w.Reset(&ResetOptions{Mode: KeepReset, Commit: commit})
	s.Require().NoError(err)

	branch, err := w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commit, branch.Hash())

	// CHANGELOG was added in the commit being reset away; it should be gone.
	_, statErr := fs.Stat("CHANGELOG")
	s.Error(statErr, "CHANGELOG should have been removed by KeepReset")
}

// TestResetKeepConflict verifies that KeepReset is aborted when a file that
// would be changed by the reset has local modifications — matching
// git reset --keep behaviour.
func (s *WorktreeSuite) TestResetKeepConflict() {
	tempDir := s.T().TempDir()
	repoDir := filepath.Join(tempDir, "repo")

	// Build a repo with two commits that both touch "tracked.txt".
	r, err := PlainInit(repoDir, false)
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.Require().NoError(err)

	commitA := CommitNewFile(s.T(), r, "tracked.txt")

	// Advance to commitB by changing tracked.txt.
	s.Require().NoError(util.WriteFile(w.filesystem, "tracked.txt", []byte("version B"), 0o644))
	_, err = w.Add("tracked.txt")
	s.Require().NoError(err)
	commitB, err := w.Commit("second", &CommitOptions{Author: defaultSignature()})
	s.Require().NoError(err)

	// Go back to commitA cleanly.
	err = w.Reset(&ResetOptions{Mode: HardReset, Commit: commitA})
	s.Require().NoError(err)

	// Make a local (unstaged) modification to tracked.txt, which differs
	// between commitA and commitB.
	s.Require().NoError(util.WriteFile(w.filesystem, "tracked.txt", []byte("local change"), 0o644))

	// KeepReset to commitB should be aborted.
	err = w.Reset(&ResetOptions{Mode: KeepReset, Commit: commitB})
	s.ErrorIs(err, ErrLocalChanges)

	// HEAD must not have moved.
	head, err := r.Head()
	s.NoError(err)
	s.Equal(commitA, head.Hash())
}

// TestResetKeepUntrackedPreserved verifies that KeepReset never deletes
// untracked files, just like real git reset --keep.
func (s *WorktreeSuite) TestResetKeepUntrackedPreserved() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.Require().NoError(err)

	// Create an untracked file.
	s.Require().NoError(util.WriteFile(fs, "untracked.txt", []byte("keep me"), 0o644))

	err = w.Reset(&ResetOptions{Mode: KeepReset, Commit: commit})
	s.Require().NoError(err)

	_, statErr := fs.Stat("untracked.txt")
	s.NoError(statErr, "untracked file was deleted by KeepReset")
}

// TestResetKeepSparselyUntracked verifies that KeepReset with SparseDirs
// removes tracked files outside the new sparse set from disk, writes files
// in the new sparse set, and preserves untracked files in the removed dirs —
// matching real git reset --keep behaviour.
func (s *WorktreeSuite) TestResetKeepSparselyUntracked() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	// Sparse-populate the "go" directory first.
	s.Require().NoError(w.Reset(&ResetOptions{Mode: KeepReset, SparseDirs: []string{"go"}}))

	goFiles, err := fs.ReadDir("go")
	s.Require().NoError(err)
	s.Require().NotEmpty(goFiles, "expected tracked files in go/ after KeepReset with SparseDirs=[go]")

	// Create an untracked file inside the soon-to-be-removed sparse dir.
	s.Require().NoError(util.WriteFile(fs, "go/untracked.txt", []byte("do not delete me"), 0o644))

	// Switch sparse set from "go" to "php" using KeepReset. There are no
	// local modifications to tracked files, so the keep-check must pass.
	s.Require().NoError(w.Reset(&ResetOptions{Mode: KeepReset, SparseDirs: []string{"php"}}))

	// Tracked files in the removed "go" sparse dir must be removed from disk;
	// git always removes SkipWorktree-marked files from the worktree.
	firstGoFile := filepath.Join("go", goFiles[0].Name())
	_, statErr := fs.Stat(firstGoFile)
	s.Require().Error(statErr, "tracked file %q in removed sparse dir should have been removed", firstGoFile)

	// Untracked file in the removed sparse dir must be preserved.
	_, statErr = fs.Stat("go/untracked.txt")
	s.Require().NoError(statErr, "untracked file inside removed sparse dir was deleted by KeepReset")

	// Files in the new sparse dir must now be present.
	phpFiles, err := fs.ReadDir("php")
	s.Require().NoError(err)
	s.NotEmpty(phpFiles, "expected php/ files after KeepReset sparse switch")
}

// TestResetKeepConflictNotOnSparseExcluded verifies that KeepReset does not
// abort when a file that changes between commits is SkipWorktree (i.e. outside
// the current sparse set). Such a file is absent from the worktree and has no
// local modifications, so it must not be counted as a conflict.
func (s *WorktreeSuite) TestResetKeepConflictNotOnSparseExcluded() {
	tempDir := s.T().TempDir()
	repoDir := filepath.Join(tempDir, "repo")

	r, err := PlainInit(repoDir, false)
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.Require().NoError(err)

	// commitA: create tracked.txt and subdir/other.txt
	commitA := CommitNewFile(s.T(), r, "tracked.txt")

	// commitB: change tracked.txt (so it appears in the tree-to-tree diff)
	s.Require().NoError(util.WriteFile(w.filesystem, "tracked.txt", []byte("version B"), 0o644))
	_, err = w.Add("tracked.txt")
	s.Require().NoError(err)
	commitB, err := w.Commit("second", &CommitOptions{Author: defaultSignature()})
	s.Require().NoError(err)

	// Go back to commitA and apply a sparse checkout that excludes tracked.txt
	// by resetting with a SparseDirs that doesn't contain it.
	// We use a subdirectory "subdir" that doesn't actually exist in the tree;
	// we skip validation so we can force SkipWorktree onto all files.
	err = w.Reset(&ResetOptions{
		Mode:                    HardReset,
		Commit:                  commitA,
		SparseDirs:              []string{"subdir"},
		SkipSparseDirValidation: true,
	})
	s.Require().NoError(err)

	// Now tracked.txt should be SkipWorktree=true (not on disk). A KeepReset
	// to commitB touches tracked.txt in the tree diff, but since it isn't
	// locally modified (it's sparse-excluded), the check must pass.
	err = w.Reset(&ResetOptions{
		Mode:                    KeepReset,
		Commit:                  commitB,
		SparseDirs:              []string{"subdir"},
		SkipSparseDirValidation: true,
	})
	s.Require().NoError(err)

	head, err := r.Head()
	s.Require().NoError(err)
	s.Equal(commitB, head.Hash(), "HEAD should advance to commitB")
}

// TestResetKeepSparseConflict verifies that KeepReset is aborted when a change
// in SparseDirs would remove a currently-on-disk tracked file that has local
// modifications — matching git reset --keep behaviour.
func (s *WorktreeSuite) TestResetKeepSparseConflict() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	// Sparse-populate the "go" directory so its tracked files are on disk.
	s.Require().NoError(w.Reset(&ResetOptions{Mode: KeepReset, SparseDirs: []string{"go"}}))

	// Find a tracked file inside go/ to dirty.
	goFiles, err := fs.ReadDir("go")
	s.Require().NoError(err)
	s.Require().NotEmpty(goFiles, "expected tracked files in go/ after sparse reset")

	dirtyFile := filepath.Join("go", goFiles[0].Name())
	s.Require().NoError(util.WriteFile(fs, dirtyFile, []byte("local modification"), 0o644))

	// Switching SparseDirs from "go" to "php" (same commit) would remove
	// dirty go/ files from disk — KeepReset must refuse.
	err = w.Reset(&ResetOptions{Mode: KeepReset, SparseDirs: []string{"php"}})
	s.ErrorIs(err, ErrLocalChanges, "KeepReset should abort when SparseDirs removal would discard local mods")
}

// TestResetKeepUntrackedOverwrite verifies that KeepReset is aborted when an
// untracked file exists at a path that the target commit would write — matching
// git reset --keep behaviour (would otherwise silently overwrite the file).
func (s *WorktreeSuite) TestResetKeepUntrackedOverwrite() {
	tempDir := s.T().TempDir()
	repoDir := filepath.Join(tempDir, "repo")

	r, err := PlainInit(repoDir, false)
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()
	w, err := r.Worktree()
	s.Require().NoError(err)

	// commitA: only tracked.txt exists.
	commitA := CommitNewFile(s.T(), r, "tracked.txt")

	// commitB: add new.txt alongside tracked.txt.
	s.Require().NoError(util.WriteFile(w.filesystem, "new.txt", []byte("tracked content"), 0o644))
	_, err = w.Add("new.txt")
	s.Require().NoError(err)
	commitB, err := w.Commit("add new.txt", &CommitOptions{Author: defaultSignature()})
	s.Require().NoError(err)

	// Go back to commitA so new.txt is absent from the worktree.
	err = w.Reset(&ResetOptions{Mode: HardReset, Commit: commitA})
	s.Require().NoError(err)

	// Place an untracked file at the path that commitB will insert.
	s.Require().NoError(util.WriteFile(w.filesystem, "new.txt", []byte("untracked content"), 0o644))

	// KeepReset to commitB must abort because new.txt would be overwritten.
	err = w.Reset(&ResetOptions{Mode: KeepReset, Commit: commitB})
	s.ErrorIs(err, ErrLocalChanges, "KeepReset should abort when an untracked file would be overwritten")

	// HEAD must not have moved.
	head, err := r.Head()
	s.NoError(err)
	s.Equal(commitA, head.Hash())
}

func (s *WorktreeSuite) TestResetHard() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	commit := plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	f, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("foo"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	err = w.Reset(&ResetOptions{Mode: HardReset, Commit: commit})
	s.NoError(err)

	branch, err := w.r.Reference(plumbing.Master, false)
	s.NoError(err)
	s.Equal(commit, branch.Hash())
}

func (s *WorktreeSuite) TestResetHardSubFolders() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = fs.MkdirAll("dir", os.ModePerm)
	s.NoError(err)
	tf, err := fs.Create("dir/testfile.txt")
	s.NoError(err)
	_, err = tf.Write([]byte("testfile content"))
	s.NoError(err)
	err = tf.Close()
	s.NoError(err)
	_, err = w.Add("dir/testfile.txt")
	s.NoError(err)
	_, err = w.Commit("testcommit", &CommitOptions{Author: &object.Signature{Name: "name", Email: "email"}})
	s.NoError(err)

	err = fs.Remove("dir/testfile.txt")
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())

	err = w.Reset(&ResetOptions{Files: []string{"./dir/testfile.txt"}, Mode: HardReset})
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestResetHardWithGitIgnore() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	tf, err := fs.Create("newTestFile.txt")
	s.NoError(err)
	_, err = tf.Write([]byte("testfile content"))
	s.NoError(err)
	err = tf.Close()
	s.NoError(err)
	_, err = w.Add("newTestFile.txt")
	s.NoError(err)
	_, err = w.Commit("testcommit", &CommitOptions{Author: &object.Signature{Name: "name", Email: "email"}})
	s.NoError(err)

	err = fs.Remove("newTestFile.txt")
	s.NoError(err)
	f, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("foo\n"))
	s.NoError(err)
	_, err = f.Write([]byte("newTestFile.txt\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())

	err = w.Reset(&ResetOptions{Mode: HardReset})
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

// TestResetHardTrackedFileInIgnoredDir covers the case the IgnoreMatcher
// optimization actually targets: a tracked file living inside a gitignored
// directory. The walker must descend into the directory because trackedDirs
// records it as containing tracked descendants, so the Delete-on-disk shows
// up as a change and HardReset restores the file.
func (s *WorktreeSuite) TestResetHardTrackedFileInIgnoredDir() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = fs.MkdirAll("vendor", os.ModePerm)
	s.NoError(err)
	tf, err := fs.Create("vendor/keep.txt")
	s.NoError(err)
	_, err = tf.Write([]byte("tracked content"))
	s.NoError(err)
	err = tf.Close()
	s.NoError(err)
	_, err = w.Add("vendor/keep.txt")
	s.NoError(err)

	gi, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = gi.Write([]byte("vendor/\n"))
	s.NoError(err)
	err = gi.Close()
	s.NoError(err)
	_, err = w.Add(".gitignore")
	s.NoError(err)

	_, err = w.Commit("testcommit", &CommitOptions{Author: &object.Signature{Name: "name", Email: "email"}})
	s.NoError(err)

	err = fs.Remove("vendor/keep.txt")
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())

	err = w.Reset(&ResetOptions{Mode: HardReset})
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	s.True(status.IsClean())

	got, err := util.ReadFile(fs, "vendor/keep.txt")
	s.NoError(err)
	s.Equal("tracked content", string(got))
}

// TestResetHardModifiedTrackedFileWithGitIgnore complements
// TestResetHardWithGitIgnore by exercising modification (not deletion) of a
// tracked file whose name is gitignored. The pre-#2048 post-walk filter
// would have dropped this Modify action; with the noder-based matcher,
// tracked entries are preserved regardless of ignore rules.
func (s *WorktreeSuite) TestResetHardModifiedTrackedFileWithGitIgnore() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	tf, err := fs.Create("tracked.txt")
	s.NoError(err)
	_, err = tf.Write([]byte("v1"))
	s.NoError(err)
	err = tf.Close()
	s.NoError(err)
	_, err = w.Add("tracked.txt")
	s.NoError(err)

	gi, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = gi.Write([]byte("tracked.txt\n"))
	s.NoError(err)
	err = gi.Close()
	s.NoError(err)
	_, err = w.Add(".gitignore")
	s.NoError(err)

	_, err = w.Commit("testcommit", &CommitOptions{Author: &object.Signature{Name: "name", Email: "email"}})
	s.NoError(err)

	err = util.WriteFile(fs, "tracked.txt", []byte("v2 dirty"), 0o644)
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())

	err = w.Reset(&ResetOptions{Mode: HardReset})
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	s.True(status.IsClean())

	got, err := util.ReadFile(fs, "tracked.txt")
	s.NoError(err)
	s.Equal("v1", string(got))
}

// TestMergeResetRemovesTrackedFileInIgnoredDir is the regression test for
// the case Copilot flagged on this PR. resetIndex runs immediately before
// resetWorktree and removes paths from the index, so a tracked path inside
// a gitignored directory that is being removed by the reset would no
// longer be in idxMap. If the noder's IgnoreMatcher were engaged here, it
// would prune the path during the walk and the Delete action needed to
// remove the file from disk would never be emitted. resetWorktree must
// keep excludeIgnoredChanges=false so MergeReset still cleans the file up.
func (s *WorktreeSuite) TestMergeResetRemovesTrackedFileInIgnoredDir() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	gi, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = gi.Write([]byte("vendor/\n"))
	s.NoError(err)
	err = gi.Close()
	s.NoError(err)
	_, err = w.Add(".gitignore")
	s.NoError(err)

	err = fs.MkdirAll("vendor", os.ModePerm)
	s.NoError(err)
	tf, err := fs.Create("vendor/keep.txt")
	s.NoError(err)
	_, err = tf.Write([]byte("tracked content"))
	s.NoError(err)
	err = tf.Close()
	s.NoError(err)
	_, err = w.Add("vendor/keep.txt")
	s.NoError(err)
	withFile, err := w.Commit("with file", &CommitOptions{Author: &object.Signature{Name: "name", Email: "email"}})
	s.NoError(err)

	_, err = w.Remove("vendor/keep.txt")
	s.NoError(err)
	withoutFile, err := w.Commit("without file", &CommitOptions{Author: &object.Signature{Name: "name", Email: "email"}})
	s.NoError(err)

	// HardReset back to "with file" so the tracked, gitignored path is on disk.
	err = w.Reset(&ResetOptions{Mode: HardReset, Commit: withFile})
	s.NoError(err)
	_, err = fs.Stat("vendor/keep.txt")
	s.NoError(err)

	// MergeReset to "without file" must remove the path even though the
	// directory matches an ignore rule.
	err = w.Reset(&ResetOptions{Mode: MergeReset, Commit: withoutFile})
	s.NoError(err)
	_, err = fs.Stat("vendor/keep.txt")
	s.True(os.IsNotExist(err), "vendor/keep.txt must be removed after MergeReset, got err=%v", err)
}

func (s *WorktreeSuite) TestResetSparsely() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	sparseResetDirs := []string{"php"}

	err := w.Reset(&ResetOptions{Mode: HardReset, SparseDirs: sparseResetDirs})
	s.NoError(err)

	files, err := fs.ReadDir("/")
	s.NoError(err)
	s.Len(files, 1)
	s.Equal("php", files[0].Name())

	files, err = fs.ReadDir("/php")
	s.NoError(err)
	s.Len(files, 1)
	s.Equal("crappy.php", files[0].Name())
}

func (s *WorktreeSuite) TestResetSparselyInvalidDir() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	tests := []struct {
		name    string
		opts    ResetOptions
		wantErr bool
	}{
		{
			name:    "non existent directory",
			opts:    ResetOptions{SparseDirs: []string{"non-existent"}},
			wantErr: true,
		},
		{
			name:    "exists but is not directory",
			opts:    ResetOptions{SparseDirs: []string{"php/crappy.php"}},
			wantErr: true,
		},
		{
			name:    "skip validation for non existent directory",
			opts:    ResetOptions{SparseDirs: []string{"non-existent"}, SkipSparseDirValidation: true},
			wantErr: false,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			err := w.Reset(&test.opts)
			if test.wantErr {
				s.Require().ErrorIs(err, ErrSparseResetDirectoryNotFound)
				return
			}
			s.Require().NoError(err)
		})
	}
}

func (s *WorktreeSuite) TestStatusAfterCheckout() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *WorktreeSuite) TestStatusAfterSparseCheckout() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{
		SparseCheckoutDirectories: []string{"php"},
		Force:                     true,
	})
	s.Require().NoError(err)

	status, err := w.Status()
	s.Require().NoError(err)
	s.True(status.IsClean(), status)
}

func (s *WorktreeSuite) TestStatusModified() {
	fs := s.TemporalFilesystem()

	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	f, err := fs.Create(".gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("foo"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())
	s.Equal(Modified, status.File(".gitignore").Worktree)
}

func (s *WorktreeSuite) TestStatusIgnored() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	w.Checkout(&CheckoutOptions{})

	fs.MkdirAll("another", os.ModePerm)
	f, _ := fs.Create("another/file")
	f.Close()
	fs.MkdirAll("vendor/github.com", os.ModePerm)
	f, _ = fs.Create("vendor/github.com/file")
	f.Close()
	fs.MkdirAll("vendor/gopkg.in", os.ModePerm)
	f, _ = fs.Create("vendor/gopkg.in/file")
	f.Close()

	status, _ := w.Status()
	s.Len(status, 3)
	_, ok := status["another/file"]
	s.True(ok)
	_, ok = status["vendor/github.com/file"]
	s.True(ok)
	_, ok = status["vendor/gopkg.in/file"]
	s.True(ok)

	f, _ = fs.Create(".gitignore")
	f.Write([]byte("vendor/g*/"))
	f.Close()
	f, _ = fs.Create("vendor/.gitignore")
	f.Write([]byte("!github.com/\n"))
	f.Close()

	status, _ = w.Status()
	s.Len(status, 4)
	_, ok = status[".gitignore"]
	s.True(ok)
	_, ok = status["another/file"]
	s.True(ok)
	_, ok = status["vendor/.gitignore"]
	s.True(ok)
	_, ok = status["vendor/github.com/file"]
	s.True(ok)
}

func (s *WorktreeSuite) TestStatusUntracked() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	f, err := w.filesystem.Create("foo")
	s.NoError(err)
	s.Nil(f.Close())

	status, err := w.Status()
	s.NoError(err)
	s.Equal(Untracked, status.File("foo").Staging)
	s.Equal(Untracked, status.File("foo").Worktree)
}

func (s *WorktreeSuite) TestStatusDeleted() {
	fs := s.TemporalFilesystem()

	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = fs.Remove(".gitignore")
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.False(status.IsClean())
	s.Equal(Deleted, status.File(".gitignore").Worktree)
}

func (s *WorktreeSuite) TestStatusFileMode() {
	runTest := func(t *testing.T, fileMode bool) string {
		fs := memfs.New()
		r, err := Init(memory.NewStorage(), WithWorkTree(fs))
		defer func() { _ = r.Close() }()
		require.NoError(t, err)

		w, err := r.Worktree()
		require.NoError(t, err)

		cfg, err := r.Config()
		require.NoError(t, err)

		cfg.Core.FileMode = fileMode
		err = r.SetConfig(cfg)
		require.NoError(t, err)

		err = util.WriteFile(fs, "run.bash", []byte("#!/bin/bash\n"), 0o0755)
		require.NoError(t, err)

		_, err = w.Add("run.bash")
		require.NoError(t, err)

		_, err = w.Commit("Add an executable", defaultTestCommitOptions())
		require.NoError(t, err)

		err = util.RemoveAll(fs, "run.bash")
		require.NoError(t, err)

		err = util.WriteFile(fs, "run.bash", []byte("#!/bin/bash\n"), 0o0644)
		require.NoError(t, err)

		s, err := w.Status()
		require.NoError(t, err)

		return s.String()
	}

	s.Run("filemode=true", func() {
		result := runTest(s.T(), true)
		s.Equal(" M run.bash\n", result)
	})

	s.Run("filemode=false", func() {
		result := runTest(s.T(), false)
		s.Equal("", result)
	})
}

func (s *WorktreeSuite) TestSubmodule() {
	fs, err := fixtures.ByTag("submodule").One().Worktree()
	s.Require().NoError(err)
	gitdir, err := fs.Chroot(GitDirName)
	s.Require().NoError(err)

	r, err := Open(filesystem.NewStorage(gitdir, cache.NewObjectLRUDefault()), fs)
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.Require().NoError(err)

	m, err := w.Submodule("basic")
	s.NoError(err)

	s.Equal("basic", m.Config().Name)
}

func (s *WorktreeSuite) TestSubmodules() {
	r := s.NewRepository(fixtures.ByTag("submodule").One())
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.Require().NoError(err)

	l, err := w.Submodules()
	s.NoError(err)

	s.Len(l, 2)
}

func (s *WorktreeSuite) TestSubmodules_URLResolution() {
	tests := []struct {
		name       string
		originURL  string
		gitmodules string
		expected   map[string]string
	}{
		{
			name:      "HTTPS origin with relative URL",
			originURL: "https://github.com/user/parent-repo.git",
			gitmodules: `[submodule "relative"]
	path = relative
	url = ../relative-repo.git`,
			expected: map[string]string{
				"relative": "https://github.com/user/relative-repo.git",
			},
		},
		{
			name:      "SSH origin with relative URL",
			originURL: "git@github.com:user/parent-repo.git",
			gitmodules: `[submodule "relative"]
	path = relative
	url = ../relative-repo.git`,
			expected: map[string]string{
				"relative": "git@github.com:user/relative-repo.git",
			},
		},
		{
			name:      "HTTPS origin with mixed relative and absolute URLs",
			originURL: "https://github.com/user/parent-repo.git",
			gitmodules: `[submodule "relative"]
	path = relative
	url = ../relative-repo.git
[submodule "absolute"]
	path = absolute
	url = https://github.com/other/absolute-repo.git`,
			expected: map[string]string{
				"relative": "https://github.com/user/relative-repo.git",
				"absolute": "https://github.com/other/absolute-repo.git",
			},
		},
		{
			name:      "file protocol with relative URL",
			originURL: "file:///home/user/parent-repo.git",
			gitmodules: `[submodule "relative"]
	path = relative
	url = ../relative-repo.git`,
			expected: map[string]string{
				"relative": "file:///home/user/relative-repo.git",
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			fs := memfs.New()
			r, err := Init(memory.NewStorage(), WithWorkTree(fs))
			defer func() { _ = r.Close() }()
			s.NoError(err)

			_, err = r.CreateRemote(&config.RemoteConfig{
				Name: "origin",
				URLs: []string{tc.originURL},
			})
			s.NoError(err)

			w, err := r.Worktree()
			s.NoError(err)

			err = util.WriteFile(fs, ".gitmodules", []byte(tc.gitmodules), 0o644)
			s.NoError(err)

			submodules, err := w.Submodules()
			s.NoError(err)
			s.Len(submodules, len(tc.expected))

			submoduleMap := make(map[string]*Submodule)
			for _, sub := range submodules {
				submoduleMap[sub.Config().Name] = sub
			}

			for name, expectedURL := range tc.expected {
				sub := submoduleMap[name]
				s.NotNil(sub, "submodule %s should exist", name)
				s.Equal(expectedURL, sub.Config().URL)
			}
		})
	}
}

func TestResolveModuleURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		originURL string
		moduleURL string
		want      string
		wantErr   error
	}{
		{
			originURL: "https://github/user/repo1",
			moduleURL: "../repo2",
			want:      "https://github/user/repo2",
		},
		{
			originURL: "https://github/user/repo1",
			moduleURL: "https://github/user/repo3",
			want:      "https://github/user/repo3",
		},
		{
			originURL: "ssh://git@github.com:22/user/repo1.git",
			moduleURL: "../repo2.git",
			want:      "ssh://git@github.com:22/user/repo2.git",
		},
		{
			originURL: "ssh://git@github.com:22/user/repo1.git",
			moduleURL: "ssh://git@github.com:22/user/repo3.git",
			want:      "ssh://git@github.com:22/user/repo3.git",
		},
		{
			originURL: "git@github.com:2222:user/repo1.git",
			moduleURL: "../repo2.git",
			want:      "git@github.com:2222:user/repo2.git",
		},
		{
			originURL: "git@github.com:user/repo1.git",
			moduleURL: "../repo2.git",
			want:      "git@github.com:user/repo2.git",
		},
		{
			originURL: "git@github.com:user/repo1.git",
			moduleURL: "git@github.com:user/repo3.git",
			want:      "git@github.com:user/repo3.git",
		},
		{
			originURL: "file://git/user/repo1.git",
			moduleURL: "../repo2.git",
			want:      "file://git/user/repo2.git",
		},
		{
			originURL: "file://git/user/repo1.git",
			moduleURL: "file://git/user/repo3.git",
			want:      "file://git/user/repo3.git",
		},
		{
			originURL: "/git/user/repo1.git",
			moduleURL: "../repo2.git",
			want:      "/git/user/repo2.git",
		},
		{
			originURL: "/git/user/repo1.git",
			moduleURL: "/git/user/repo3.git",
			want:      "/git/user/repo3.git",
		},
		{
			originURL: "",
			moduleURL: "../repo2/.git",
			want:      "../repo2/.git",
		},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got, err := resolveModuleURL(tc.originURL, tc.moduleURL)
			if tc.wantErr == nil {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			} else {
				assert.Empty(t, got)
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func (s *WorktreeSuite) TestAddUntracked() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "foo", []byte("FOO"), 0o755)
	s.NoError(err)

	hash, err := w.Add("foo")
	s.Equal("d96c7efbfec2814ae0301ad054dc8d9fc416c9b5", hash.String())
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 10)

	e, err := idx.Entry("foo")
	s.NoError(err)
	s.Equal(hash, e.Hash)
	s.Equal(filemode.Executable, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)

	file := status.File("foo")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)

	obj, err := w.r.Storer.EncodedObject(plumbing.BlobObject, hash)
	s.NoError(err)
	s.NotNil(obj)
	s.Equal(int64(3), obj.Size())
}

func (s *WorktreeSuite) TestAddAbsolutePath() {
	dir := s.T().TempDir()

	r, err := PlainInit(dir, false)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	s.NoError(err)

	err = util.WriteFile(w.filesystem, "foo.txt", []byte("FOO"), 0o644)
	s.NoError(err)

	absPath := filepath.Join(dir, "foo.txt")
	_, err = w.Add(absPath)
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)

	_, err = idx.Entry("foo.txt")
	s.NoError(err)

	_, err = idx.Entry(absPath)
	s.Error(err)

	status, err := w.Status()
	s.NoError(err)

	file := status.File("foo.txt")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestAddCRLF() {
	runTest := func(t *testing.T, autoCRLF string) (result []byte) {
		r := NewRepositoryWithEmptyWorktree(fixtures.Basic().One())
		defer func() { _ = r.Close() }()

		cfg, err := r.Config()
		require.NoError(t, err)
		cfg.Core.AutoCRLF = autoCRLF
		require.NoError(t, r.SetConfig(cfg))

		wt, err := r.Worktree()
		require.NoError(t, err)

		err = util.WriteFile(wt.filesystem, "foo", []byte("FOO\r\n"), 0o755)
		require.NoError(t, err)

		h, err := wt.Add("foo")
		require.NoError(t, err)

		obj, err := r.Storer.EncodedObject(plumbing.BlobObject, h)
		require.NoError(t, err)
		require.NotNil(t, obj)

		reader, err := obj.Reader()
		require.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		s.Require().NoError(err)

		return content
	}

	s.Run("autocrlf=true", func() {
		result := runTest(s.T(), "true")
		s.Equal("FOO\n", string(result))
	})

	s.Run("autocrlf=input", func() {
		result := runTest(s.T(), "input")
		s.Equal("FOO\n", string(result))
	})
}

func (s *WorktreeSuite) TestIgnored() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	w.Excludes = make([]gitignore.Pattern, 0)
	w.Excludes = append(w.Excludes, gitignore.ParsePattern("foo", nil))

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "foo", []byte("FOO"), 0o755)
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 0)

	file := status.File("foo")
	s.Equal(Untracked, file.Staging)
	s.Equal(Untracked, file.Worktree)
}

func (s *WorktreeSuite) TestExcludedNoGitignore() {
	f := fixtures.ByTag("empty").One()
	r := s.NewRepository(f)
	defer func() { _ = r.Close() }()

	fs := memfs.New()
	w := &Worktree{
		r:          r,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	_, err := fs.Open(".gitignore")
	s.ErrorIs(err, os.ErrNotExist)

	w.Excludes = make([]gitignore.Pattern, 0)
	w.Excludes = append(w.Excludes, gitignore.ParsePattern("foo", nil))

	err = util.WriteFile(w.filesystem, "foo", []byte("FOO"), 0o755)
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 0)

	file := status.File("foo")
	s.Equal(Untracked, file.Staging)
	s.Equal(Untracked, file.Worktree)
}

func (s *WorktreeSuite) TestAddModified() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "LICENSE", []byte("FOO"), 0o644)
	s.NoError(err)

	hash, err := w.Add("LICENSE")
	s.NoError(err)
	s.Equal("d96c7efbfec2814ae0301ad054dc8d9fc416c9b5", hash.String())

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	e, err := idx.Entry("LICENSE")
	s.NoError(err)
	s.Equal(hash, e.Hash)
	s.Equal(filemode.Regular, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)

	file := status.File("LICENSE")
	s.Equal(Modified, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestAddUnmodified() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Add("LICENSE")
	s.Equal("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", hash.String())
	s.NoError(err)
}

func (s *WorktreeSuite) TestAddRemoved() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = w.filesystem.Remove("LICENSE")
	s.NoError(err)

	hash, err := w.Add("LICENSE")
	s.NoError(err)
	s.Equal("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", hash.String())

	e, err := idx.Entry("LICENSE")
	s.NoError(err)
	s.Equal(hash, e.Hash)
	s.Equal(filemode.Regular, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)

	file := status.File("LICENSE")
	s.Equal(Deleted, file.Staging)
}

func (s *WorktreeSuite) testAddRemovedInDirectory(addPath string, expectedJSONStatus StatusCode) {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = w.filesystem.Remove("go/example.go")
	s.NoError(err)

	err = w.filesystem.Remove("json/short.json")
	s.NoError(err)

	hash, err := w.Add(addPath)
	s.NoError(err)
	s.True(hash.IsZero())

	e, err := idx.Entry("go/example.go")
	s.NoError(err)
	s.Equal(plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"), e.Hash)
	s.Equal(filemode.Regular, e.Mode)

	e, err = idx.Entry("json/short.json")
	s.NoError(err)
	s.Equal(plumbing.NewHash("c8f1d8c61f9da76f4cb49fd86322b6e685dba956"), e.Hash)
	s.Equal(filemode.Regular, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 2)

	file := status.File("go/example.go")
	s.Equal(Deleted, file.Staging)

	file = status.File("json/short.json")
	s.Equal(expectedJSONStatus, file.Staging)
}

func (s *WorktreeSuite) TestAddRemovedInDirectory() {
	s.testAddRemovedInDirectory("go", Unmodified)
}

func (s *WorktreeSuite) TestAddRemovedInDirectoryWithTrailingSlash() {
	s.testAddRemovedInDirectory("go/", Unmodified)
}

func (s *WorktreeSuite) TestAddRemovedInDirectoryDot() {
	s.testAddRemovedInDirectory(".", Deleted)
}

func (s *WorktreeSuite) TestAddSymlink() {
	dir := s.T().TempDir()

	r, err := PlainInit(dir, false)
	s.NoError(err)
	defer func() { _ = r.Close() }()
	err = util.WriteFile(r.wt, "foo", []byte("qux"), 0o644)
	s.NoError(err)
	err = r.wt.Symlink("foo", "bar")
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)
	h, err := w.Add("foo")
	s.NoError(err)
	s.NotEqual(plumbing.NewHash("19102815663d23f8b75a47e7a01965dcdc96468c"), h)

	h, err = w.Add("bar")
	s.NoError(err)
	s.Equal(plumbing.NewHash("19102815663d23f8b75a47e7a01965dcdc96468c"), h)

	obj, err := w.r.Storer.EncodedObject(plumbing.BlobObject, h)
	s.NoError(err)
	s.NotNil(obj)
	s.Equal(int64(3), obj.Size())
}

func (s *WorktreeSuite) TestAddDirectory() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "qux/foo", []byte("FOO"), 0o755)
	s.NoError(err)
	err = util.WriteFile(w.filesystem, "qux/baz/bar", []byte("BAR"), 0o755)
	s.NoError(err)

	h, err := w.Add("qux")
	s.NoError(err)
	s.True(h.IsZero())

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 11)

	e, err := idx.Entry("qux/foo")
	s.NoError(err)
	s.Equal(filemode.Executable, e.Mode)

	e, err = idx.Entry("qux/baz/bar")
	s.NoError(err)
	s.Equal(filemode.Executable, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 2)

	file := status.File("qux/foo")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)

	file = status.File("qux/baz/bar")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestAddDirectoryErrorNotFound() {
	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()
	w, _ := r.Worktree()

	h, err := w.Add("foo")
	s.NotNil(err)
	s.True(h.IsZero())
}

func (s *WorktreeSuite) TestAddAll() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "file1", []byte("file1"), 0o644)
	s.NoError(err)

	err = util.WriteFile(w.filesystem, "file2", []byte("file2"), 0o644)
	s.NoError(err)

	err = util.WriteFile(w.filesystem, "file3", []byte("ignore me"), 0o644)
	s.NoError(err)

	w.Excludes = make([]gitignore.Pattern, 0)
	w.Excludes = append(w.Excludes, gitignore.ParsePattern("file3", nil))

	err = w.AddWithOptions(&AddOptions{All: true})
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 11)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 2)

	file1 := status.File("file1")
	s.Equal(Added, file1.Staging)
	file2 := status.File("file2")
	s.Equal(Added, file2.Staging)
	file3 := status.File("file3")
	s.Equal(Untracked, file3.Staging)
	s.Equal(Untracked, file3.Worktree)
}

func (s *WorktreeSuite) TestAddGlob() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "qux/qux", []byte("QUX"), 0o755)
	s.NoError(err)
	err = util.WriteFile(w.filesystem, "qux/baz", []byte("BAZ"), 0o755)
	s.NoError(err)
	err = util.WriteFile(w.filesystem, "qux/bar/baz", []byte("BAZ"), 0o755)
	s.NoError(err)

	err = w.AddWithOptions(&AddOptions{Glob: w.filesystem.Join("qux", "b*")})
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 11)

	e, err := idx.Entry("qux/baz")
	s.NoError(err)
	s.Equal(filemode.Executable, e.Mode)

	e, err = idx.Entry("qux/bar/baz")
	s.NoError(err)
	s.Equal(filemode.Executable, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 3)

	file := status.File("qux/qux")
	s.Equal(Untracked, file.Staging)
	s.Equal(Untracked, file.Worktree)

	file = status.File("qux/baz")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)

	file = status.File("qux/bar/baz")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestAddFilenameStartingWithDot() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, "qux", []byte("QUX"), 0o755)
	s.NoError(err)
	err = util.WriteFile(w.filesystem, "baz", []byte("BAZ"), 0o755)
	s.NoError(err)
	err = util.WriteFile(w.filesystem, "foo/bar/baz", []byte("BAZ"), 0o755)
	s.NoError(err)

	_, err = w.Add("./qux")
	s.NoError(err)

	_, err = w.Add("./baz")
	s.NoError(err)

	_, err = w.Add("foo/bar/../bar/./baz")
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 12)

	e, err := idx.Entry("qux")
	s.NoError(err)
	s.Equal(filemode.Executable, e.Mode)

	e, err = idx.Entry("baz")
	s.NoError(err)
	s.Equal(filemode.Executable, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 3)

	file := status.File("qux")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)

	file = status.File("baz")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)

	file = status.File("foo/bar/baz")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestAddGlobErrorNoMatches() {
	r, _ := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	defer func() { _ = r.Close() }()
	w, _ := r.Worktree()

	err := w.AddGlob("foo")
	s.ErrorIs(err, ErrGlobNoMatches)
}

func (s *WorktreeSuite) testAddSkipStatus(filePath string, expectedEntries int, expectedStaging StatusCode) {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(w.filesystem, filePath, []byte("file1"), 0o644)
	s.NoError(err)

	err = w.AddWithOptions(&AddOptions{Path: filePath, SkipStatus: true})
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, expectedEntries)

	e, err := idx.Entry(filePath)
	s.NoError(err)
	s.Equal(filemode.Regular, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)

	file := status.File(filePath)
	s.Equal(expectedStaging, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestAddSkipStatusAddedPath() {
	s.testAddSkipStatus("file1", 10, Added)
}

func (s *WorktreeSuite) TestAddSkipStatusModifiedPath() {
	s.testAddSkipStatus("LICENSE", 9, Modified)
}

func (s *WorktreeSuite) TestAddSkipStatusNonModifiedPath() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = w.AddWithOptions(&AddOptions{Path: "LICENSE", SkipStatus: true})
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	e, err := idx.Entry("LICENSE")
	s.NoError(err)
	s.Equal(filemode.Regular, e.Mode)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 0)

	file := status.File("LICENSE")
	s.Equal(Untracked, file.Staging)
	s.Equal(Untracked, file.Worktree)
}

func (s *WorktreeSuite) TestAddSkipStatusWithIgnoredPath() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	idx, err := w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 9)

	err = util.WriteFile(fs, ".gitignore", []byte("fileToIgnore\n"), 0o755)
	s.NoError(err)
	_, err = w.Add(".gitignore")
	s.NoError(err)
	_, err = w.Commit("Added .gitignore", defaultTestCommitOptions())
	s.NoError(err)

	err = util.WriteFile(fs, "fileToIgnore", []byte("file to ignore"), 0o644)
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 0)

	file := status.File("fileToIgnore")
	s.Equal(Untracked, file.Staging)
	s.Equal(Untracked, file.Worktree)

	err = w.AddWithOptions(&AddOptions{Path: "fileToIgnore", SkipStatus: true})
	s.NoError(err)

	idx, err = w.r.Storer.Index()
	s.NoError(err)
	s.Len(idx.Entries, 10)

	e, err := idx.Entry("fileToIgnore")
	s.NoError(err)
	s.Equal(filemode.Regular, e.Mode)

	status, err = w.Status()
	s.NoError(err)
	s.Len(status, 1)

	file = status.File("fileToIgnore")
	s.Equal(Added, file.Staging)
	s.Equal(Unmodified, file.Worktree)
}

func (s *WorktreeSuite) TestRemove() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Remove("LICENSE")
	s.Equal("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", hash.String())
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)
	s.Equal(Deleted, status.File("LICENSE").Staging)
}

func (s *WorktreeSuite) TestRemoveNotExistentEntry() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Remove("not-exists")
	s.True(hash.IsZero())
	s.NotNil(err)
}

func (s *WorktreeSuite) TestRemoveDirectory() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Remove("json")
	s.True(hash.IsZero())
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 2)
	s.Equal(Deleted, status.File("json/long.json").Staging)
	s.Equal(Deleted, status.File("json/short.json").Staging)

	_, err = w.filesystem.Stat("json")
	s.True(os.IsNotExist(err))
}

func (s *WorktreeSuite) TestRemoveDirectoryUntracked() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	err = util.WriteFile(w.filesystem, "json/foo", []byte("FOO"), 0o755)
	s.NoError(err)

	hash, err := w.Remove("json")
	s.True(hash.IsZero())
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 3)
	s.Equal(Deleted, status.File("json/long.json").Staging)
	s.Equal(Deleted, status.File("json/short.json").Staging)
	s.Equal(Untracked, status.File("json/foo").Staging)

	_, err = w.filesystem.Stat("json")
	s.NoError(err)
}

func (s *WorktreeSuite) TestRemoveDeletedFromWorktree() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	err = fs.Remove("LICENSE")
	s.NoError(err)

	hash, err := w.Remove("LICENSE")
	s.Equal("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", hash.String())
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)
	s.Equal(Deleted, status.File("LICENSE").Staging)
}

func (s *WorktreeSuite) TestRemoveGlob() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	err = w.RemoveGlob(w.filesystem.Join("json", "l*"))
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 1)
	s.Equal(Deleted, status.File("json/long.json").Staging)
}

func (s *WorktreeSuite) TestRemoveGlobDirectory() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	err = w.RemoveGlob("js*")
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 2)
	s.Equal(Deleted, status.File("json/short.json").Staging)
	s.Equal(Deleted, status.File("json/long.json").Staging)

	_, err = w.filesystem.Stat("json")
	s.True(os.IsNotExist(err))
}

func (s *WorktreeSuite) TestRemoveGlobDirectoryDeleted() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	err = fs.Remove("json/short.json")
	s.NoError(err)

	err = util.WriteFile(w.filesystem, "json/foo", []byte("FOO"), 0o755)
	s.NoError(err)

	err = w.RemoveGlob("js*")
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 3)
	s.Equal(Deleted, status.File("json/short.json").Staging)
	s.Equal(Deleted, status.File("json/long.json").Staging)
}

func (s *WorktreeSuite) TestMove() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Move("LICENSE", "foo")
	s.Equal("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", hash.String())
	s.NoError(err)

	status, err := w.Status()
	s.NoError(err)
	s.Len(status, 2)
	s.Equal(Deleted, status.File("LICENSE").Staging)
	s.Equal(Added, status.File("foo").Staging)
}

func (s *WorktreeSuite) TestMoveNotExistentEntry() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Move("not-exists", "foo")
	s.True(hash.IsZero())
	s.NotNil(err)
}

func (s *WorktreeSuite) TestMoveToExistent() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{Force: true})
	s.NoError(err)

	hash, err := w.Move(".gitignore", "LICENSE")
	s.True(hash.IsZero())
	s.ErrorIs(err, ErrDestinationExists)
}

func (s *WorktreeSuite) TestClean() {
	fs, err := fixtures.ByTag("dirty").One().Worktree(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)

	// Open the repo.
	fs, err = fs.Chroot("repo")
	s.NoError(err)
	r, err := PlainOpen(fs.Root())
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	wt, err := r.Worktree()
	s.NoError(err)

	// Status before cleaning.
	status, err := wt.Status()
	s.NoError(err)
	s.Len(status, 2)

	err = wt.Clean(&CleanOptions{})
	s.NoError(err)

	// Status after cleaning.
	status, err = wt.Status()
	s.NoError(err)

	s.Len(status, 1)

	fi, err := fs.Lstat("pkgA")
	s.NoError(err)
	s.True(fi.IsDir())

	// Clean with Dir: true.
	err = wt.Clean(&CleanOptions{Dir: true})
	s.NoError(err)

	status, err = wt.Status()
	s.NoError(err)

	s.Len(status, 0)

	// An empty dir should be deleted, as well.
	_, err = fs.Lstat("pkgA")
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *WorktreeSuite) TestCleanBare() {
	storer := memory.NewStorage()

	r, err := Init(storer)
	s.NoError(err)
	s.NotNil(r)

	wtfs := memfs.New()

	err = wtfs.MkdirAll("worktree", os.ModePerm)
	s.NoError(err)

	wtfs, err = wtfs.Chroot("worktree")
	s.NoError(err)

	_ = r.Close()
	r, err = Open(storer, wtfs)
	s.NoError(err)
	defer func() { _ = r.Close() }()

	wt, err := r.Worktree()
	s.NoError(err)

	_, err = wt.filesystem.Lstat(".")
	s.NoError(err)

	// Clean with Dir: true.
	err = wt.Clean(&CleanOptions{Dir: true})
	s.NoError(err)

	// Root worktree directory must remain after cleaning
	_, err = wt.filesystem.Lstat(".")
	s.NoError(err)
}

func TestAlternatesRepo(t *testing.T) {
	t.Parallel()
	fs, err := fixtures.ByTag("alternates").One().Worktree()
	require.NoError(t, err)

	// Open 1st repo.
	rep1fs, err := fs.Chroot("rep1")
	assert.NoError(t, err)
	d, _ := rep1fs.Chroot(GitDirName)
	rep1, err := Open(filesystem.NewStorage(d, cache.NewObjectLRUDefault()), nil)
	require.NoError(t, err)
	defer func() { _ = rep1.Close() }()

	// Open 2nd repo.
	rep2fs, err := fs.Chroot("rep2")
	assert.NoError(t, err)
	d, _ = rep2fs.Chroot(GitDirName)
	storer := filesystem.NewStorageWithOptions(d,
		cache.NewObjectLRUDefault(), filesystem.Options{
			AlternatesFS: fs,
		})
	rep2, err := Open(storer, rep2fs)
	require.NoError(t, err)
	defer func() { _ = rep2.Close() }()

	// Get the HEAD commit from the main repo.
	h, err := rep1.Head()
	assert.NoError(t, err)
	commit1, err := rep1.CommitObject(h.Hash())
	assert.NoError(t, err)

	// Get the HEAD commit from the shared repo.
	h, err = rep2.Head()
	assert.NoError(t, err)
	commit2, err := rep2.CommitObject(h.Hash())
	assert.NoError(t, err)

	assert.Equal(t, commit1.String(), commit2.String())
}

func (s *WorktreeSuite) TestGrep() {
	cases := []struct {
		name           string
		options        GrepOptions
		wantResult     []GrepResult
		dontWantResult []GrepResult
		wantError      error
	}{
		{
			name: "basic word match",
			options: GrepOptions{
				Patterns: []*regexp.Regexp{regexp.MustCompile("import")},
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		}, {
			name: "case insensitive match",
			options: GrepOptions{
				Patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)IMport`)},
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		}, {
			name: "invert match",
			options: GrepOptions{
				Patterns:    []*regexp.Regexp{regexp.MustCompile("import")},
				InvertMatch: true,
			},
			dontWantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		}, {
			name: "match at a given commit hash",
			options: GrepOptions{
				Patterns:   []*regexp.Regexp{regexp.MustCompile("The MIT License")},
				CommitHash: plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
			},
			wantResult: []GrepResult{
				{
					FileName:   "LICENSE",
					LineNumber: 1,
					Content:    "The MIT License (MIT)",
					TreeName:   "b029517f6300c2da0f4b651b8642506cd6aaf45d",
				},
			},
			dontWantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		}, {
			name: "match for a given pathspec",
			options: GrepOptions{
				Patterns:  []*regexp.Regexp{regexp.MustCompile("import")},
				PathSpecs: []*regexp.Regexp{regexp.MustCompile("go/")},
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
			dontWantResult: []GrepResult{
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		}, {
			name: "match at a given reference name",
			options: GrepOptions{
				Patterns:      []*regexp.Regexp{regexp.MustCompile("import")},
				ReferenceName: "refs/heads/master",
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "refs/heads/master",
				},
			},
		}, {
			name: "ambiguous options",
			options: GrepOptions{
				Patterns:      []*regexp.Regexp{regexp.MustCompile("import")},
				CommitHash:    plumbing.NewHash("2d55a722f3c3ecc36da919dfd8b6de38352f3507"),
				ReferenceName: "somereferencename",
			},
			wantError: ErrHashOrReference,
		}, {
			name: "multiple patterns",
			options: GrepOptions{
				Patterns: []*regexp.Regexp{
					regexp.MustCompile("import"),
					regexp.MustCompile("License"),
				},
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "LICENSE",
					LineNumber: 1,
					Content:    "The MIT License (MIT)",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		}, {
			name: "multiple pathspecs",
			options: GrepOptions{
				Patterns: []*regexp.Regexp{regexp.MustCompile("import")},
				PathSpecs: []*regexp.Regexp{
					regexp.MustCompile("go/"),
					regexp.MustCompile("vendor/"),
				},
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		},
	}

	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	server, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{URL: url})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	w, err := server.Worktree()
	s.Require().NoError(err)

	for _, tc := range cases {
		gr, err := w.Grep(&tc.options)
		if tc.wantError != nil {
			s.ErrorIs(err, tc.wantError)
		} else {
			s.NoError(err)
		}

		// Iterate through the results and check if the wanted result is present
		// in the got result.
		for _, wantResult := range tc.wantResult {
			if !slices.Contains(gr, wantResult) {
				s.T().Errorf("unexpected grep results for %q, expected result to contain: %v", tc.name, wantResult)
			}
		}

		// Iterate through the results and check if the not wanted result is
		// present in the got result.
		for _, dontWantResult := range tc.dontWantResult {
			if slices.Contains(gr, dontWantResult) {
				s.T().Errorf("unexpected grep results for %q, expected result to NOT contain: %v", tc.name, dontWantResult)
			}
		}
	}
}

func (s *WorktreeSuite) TestGrepBare() {
	cases := []struct {
		name           string
		options        GrepOptions
		wantResult     []GrepResult
		dontWantResult []GrepResult
		wantError      error
	}{
		{
			name: "basic word match",
			options: GrepOptions{
				Patterns:   []*regexp.Regexp{regexp.MustCompile("import")},
				CommitHash: plumbing.ZeroHash,
			},
			wantResult: []GrepResult{
				{
					FileName:   "go/example.go",
					LineNumber: 3,
					Content:    "import (",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
				{
					FileName:   "vendor/foo.go",
					LineNumber: 3,
					Content:    "import \"fmt\"",
					TreeName:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
				},
			},
		},
	}

	url := s.GetLocalRepositoryURL(fixtures.Basic().ByTag("worktree").One())

	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{URL: url, Bare: true})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	for _, tc := range cases {
		gr, err := r.Grep(&tc.options)
		if tc.wantError != nil {
			s.ErrorIs(err, tc.wantError)
		} else {
			s.NoError(err)
		}

		// Iterate through the results and check if the wanted result is present
		// in the got result.
		for _, wantResult := range tc.wantResult {
			if !slices.Contains(gr, wantResult) {
				s.T().Errorf("unexpected grep results for %q, expected result to contain: %v", tc.name, wantResult)
			}
		}

		// Iterate through the results and check if the not wanted result is
		// present in the got result.
		for _, dontWantResult := range tc.dontWantResult {
			if slices.Contains(gr, dontWantResult) {
				s.T().Errorf("unexpected grep results for %q, expected result to NOT contain: %v", tc.name, dontWantResult)
			}
		}
	}
}

func (s *WorktreeSuite) TestResetLingeringDirectories() {
	dir := s.T().TempDir()

	commitOpts := &CommitOptions{Author: &object.Signature{
		Name:  "foo",
		Email: "foo@foo.foo",
		When:  time.Now(),
	}}

	repo, err := PlainInit(dir, false)
	s.NoError(err)
	defer func() { _ = repo.Close() }()

	w, err := repo.Worktree()
	s.NoError(err)

	os.WriteFile(filepath.Join(dir, "README"), []byte("placeholder"), 0o644)

	_, err = w.Add(".")
	s.NoError(err)

	initialHash, err := w.Commit("Initial commit", commitOpts)
	s.NoError(err)

	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "b", "1"), []byte("1"), 0o644)

	_, err = w.Add(".")
	s.NoError(err)

	_, err = w.Commit("Add file in nested sub-directories", commitOpts)
	s.NoError(err)

	// reset to initial commit, which should remove a/b/1, a/b, and a
	err = w.Reset(&ResetOptions{
		Commit: initialHash,
		Mode:   HardReset,
	})
	s.NoError(err)

	_, err = os.Stat(filepath.Join(dir, "a", "b", "1"))
	s.True(errors.Is(err, os.ErrNotExist))

	_, err = os.Stat(filepath.Join(dir, "a", "b"))
	s.True(errors.Is(err, os.ErrNotExist))

	_, err = os.Stat(filepath.Join(dir, "a"))
	s.True(errors.Is(err, os.ErrNotExist))
}

func (s *WorktreeSuite) TestAddAndCommit() {
	expectedFiles := 2

	dir := s.T().TempDir()

	repo, err := PlainInit(dir, false)
	s.NoError(err)
	defer func() { _ = repo.Close() }()

	w, err := repo.Worktree()
	s.NoError(err)

	os.WriteFile(filepath.Join(dir, "foo"), []byte("bar"), 0o644)
	os.WriteFile(filepath.Join(dir, "bar"), []byte("foo"), 0o644)

	_, err = w.Add(".")
	s.NoError(err)

	_, err = w.Commit("Test Add And Commit", &CommitOptions{Author: &object.Signature{
		Name:  "foo",
		Email: "foo@foo.foo",
		When:  time.Now(),
	}})
	s.NoError(err)

	iter, err := w.r.Log(&LogOptions{})
	s.NoError(err)

	filesFound := 0
	err = iter.ForEach(func(c *object.Commit) error {
		files, err := c.Files()
		if err != nil {
			return err
		}

		err = files.ForEach(func(*object.File) error {
			filesFound++
			return nil
		})
		return err
	})
	s.NoError(err)
	s.Equal(expectedFiles, filesFound)
}

func (s *WorktreeSuite) TestAddAndCommitEmpty() {
	dir := s.T().TempDir()

	repo, err := PlainInit(dir, false)
	s.NoError(err)
	defer func() { _ = repo.Close() }()

	w, err := repo.Worktree()
	s.NoError(err)

	_, err = w.Add(".")
	s.NoError(err)

	_, err = w.Commit("Test Add And Commit", &CommitOptions{Author: &object.Signature{
		Name:  "foo",
		Email: "foo@foo.foo",
		When:  time.Now(),
	}})
	s.ErrorIs(err, ErrEmptyCommit)
}

func (s *WorktreeSuite) TestLinkedWorktree() {
	fs, err := fixtures.ByTag("linked-worktree").One().Worktree(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)

	// Open main repo.
	{
		fs, err := fs.Chroot("main")
		s.Require().NoError(err)
		repo, err := PlainOpenWithOptions(fs.Root(), nil)
		s.Require().NoError(err)
		defer func() { _ = repo.Close() }()

		wt, err := repo.Worktree()
		s.NoError(err)

		status, err := wt.Status()
		s.NoError(err)
		s.Len(status, 2) // 2 files

		head, err := repo.Head()
		s.NoError(err)
		s.Equal("refs/heads/master", string(head.Name()))
	}

	// Open linked-worktree #1.
	{
		fs, err := fs.Chroot("linked-worktree-1")
		s.Require().NoError(err)
		repo, err := PlainOpenWithOptions(fs.Root(), nil)
		s.Require().NoError(err)
		defer func() { _ = repo.Close() }()

		wt, err := repo.Worktree()
		s.NoError(err)

		status, err := wt.Status()
		s.NoError(err)
		s.Len(status, 3) // 3 files

		_, ok := status["linked-worktree-1-unique-file.txt"]
		s.True(ok)

		head, err := repo.Head()
		s.NoError(err)
		s.Equal("refs/heads/linked-worktree-1", string(head.Name()))
	}

	// Open linked-worktree #2.
	{
		fs, err := fs.Chroot("linked-worktree-2")
		s.Require().NoError(err)
		repo, err := PlainOpenWithOptions(fs.Root(), nil)
		s.Require().NoError(err)
		defer func() { _ = repo.Close() }()

		wt, err := repo.Worktree()
		s.NoError(err)

		status, err := wt.Status()
		s.NoError(err)
		s.Len(status, 3) // 3 files

		_, ok := status["linked-worktree-2-unique-file.txt"]
		s.True(ok)

		head, err := repo.Head()
		s.NoError(err)
		s.Equal("refs/heads/branch-with-different-name", string(head.Name()))
	}

	// Open linked-worktree #2.
	{
		fs, err := fs.Chroot("linked-worktree-invalid-commondir")
		s.Require().NoError(err)
		_, err = PlainOpenWithOptions(fs.Root(), nil)
		s.ErrorIs(err, ErrRepositoryIncomplete)
	}
}

func TestTreeContainsDirs(t *testing.T) {
	t.Parallel()
	buildTree := func() *object.Tree {
		tree := &object.Tree{
			Entries: []object.TreeEntry{
				{Name: "foo", Mode: filemode.Dir},
				{Name: "bar", Mode: filemode.Dir},
				{Name: "baz", Mode: filemode.Dir},
				{Name: "this-is-regular", Mode: filemode.Regular},
			},
		}

		sort.Sort(object.TreeEntrySorter(tree.Entries))
		return tree
	}

	tests := []struct {
		name     string
		dirs     []string
		expected bool
	}{
		{
			name:     "example",
			dirs:     []string{"foo", "baz"},
			expected: true,
		},
		{
			name:     "empty directories",
			dirs:     []string{},
			expected: false,
		},
		{
			name:     "non existent directory",
			dirs:     []string{"foobarbaz"},
			expected: false,
		},
		{
			name:     "exists but is not directory",
			dirs:     []string{"this-is-regular"},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.expected, treeContainsDirs(buildTree(), test.dirs))
		})
	}
}

var statusCodeNames = map[StatusCode]string{
	Unmodified:         "Unmodified",
	Untracked:          "Untracked",
	Modified:           "Modified",
	Added:              "Added",
	Deleted:            "Deleted",
	Renamed:            "Renamed",
	Copied:             "Copied",
	UpdatedButUnmerged: "UpdatedButUnmerged",
}

func setupForRestore(s *WorktreeSuite) (fs billy.Filesystem, w *Worktree, names []string) {
	fs = memfs.New()
	w = &Worktree{
		r:          s.Repository,
		filesystem: newWorktreeFilesystem(fs, defaultProtectNTFS(), defaultProtectHFS()),
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	names = []string{"foo", "CHANGELOG", "LICENSE", "binary.jpg"}
	verifyStatus(s, "Checkout", w, names, []FileStatus{
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
	})

	// Touch of bunch of files including create a new file and delete an existing file
	for _, name := range names {
		err = util.WriteFile(fs, name, []byte("Foo Bar"), 0o755)
		s.NoError(err)
	}
	err = util.RemoveAll(fs, names[3])
	s.NoError(err)

	// Confirm the status after doing the edits without staging anything
	verifyStatus(s, "Edits", w, names, []FileStatus{
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Modified, Staging: Unmodified},
		{Worktree: Modified, Staging: Unmodified},
		{Worktree: Deleted, Staging: Unmodified},
	})

	// Stage all files and verify the updated status
	for _, name := range names {
		_, err = w.Add(name)
		s.NoError(err)
	}
	verifyStatus(s, "Staged", w, names, []FileStatus{
		{Worktree: Unmodified, Staging: Added},
		{Worktree: Unmodified, Staging: Modified},
		{Worktree: Unmodified, Staging: Modified},
		{Worktree: Unmodified, Staging: Deleted},
	})

	// Add secondary changes to a file to make sure we only restore the staged file
	err = util.WriteFile(fs, names[1], []byte("Foo Bar:11"), 0o755)
	s.NoError(err)
	err = util.WriteFile(fs, names[2], []byte("Foo Bar:22"), 0o755)
	s.NoError(err)

	verifyStatus(s, "Secondary Edits", w, names, []FileStatus{
		{Worktree: Unmodified, Staging: Added},
		{Worktree: Modified, Staging: Modified},
		{Worktree: Modified, Staging: Modified},
		{Worktree: Unmodified, Staging: Deleted},
	})

	return fs, w, names
}

func verifyStatus(s *WorktreeSuite, marker string, w *Worktree, files []string, statuses []FileStatus) {
	s.Len(statuses, len(files))

	status, err := w.Status()
	s.NoError(err)

	for i, file := range files {
		current := status.File(file)
		expected := statuses[i]
		s.Equal(expected.Worktree, current.Worktree, fmt.Sprintf("%s - [%d] : %s Worktree %s != %s", marker, i, file, statusCodeNames[current.Worktree], statusCodeNames[expected.Worktree]))
		s.Equal(expected.Staging, current.Staging, fmt.Sprintf("%s - [%d] : %s Staging %s != %s", marker, i, file, statusCodeNames[current.Staging], statusCodeNames[expected.Staging]))
	}
}

func (s *WorktreeSuite) TestRestoreStaged() {
	fs, w, names := setupForRestore(s)

	// Attempt without files should throw an error like the git restore --staged
	opts := RestoreOptions{Staged: true}
	err := w.Restore(&opts)
	s.ErrorIs(err, ErrNoRestorePaths)

	// Restore Staged files in 2 groups and confirm status
	opts.Files = []string{names[0], "./" + names[1]}
	err = w.Restore(&opts)
	s.NoError(err)
	verifyStatus(s, "Restored First", w, names, []FileStatus{
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Modified, Staging: Unmodified},
		{Worktree: Modified, Staging: Modified},
		{Worktree: Unmodified, Staging: Deleted},
	})

	// Make sure the restore didn't overwrite our secondary changes
	contents, err := util.ReadFile(fs, names[1])
	s.NoError(err)
	s.Equal("Foo Bar:11", string(contents))

	opts.Files = []string{"./" + names[2], names[3]}
	err = w.Restore(&opts)
	s.NoError(err)
	verifyStatus(s, "Restored Second", w, names, []FileStatus{
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Modified, Staging: Unmodified},
		{Worktree: Modified, Staging: Unmodified},
		{Worktree: Deleted, Staging: Unmodified},
	})

	// Make sure the restore didn't overwrite our secondary changes
	contents, err = util.ReadFile(fs, names[2])
	s.NoError(err)
	s.Equal("Foo Bar:22", string(contents))
}

func (s *WorktreeSuite) TestRestoreWorktree() {
	_, w, names := setupForRestore(s)

	// Attempt without files should throw an error like the git restore
	opts := RestoreOptions{}
	err := w.Restore(&opts)
	s.ErrorIs(err, ErrNoRestorePaths)

	opts.Files = []string{names[0], names[1]}
	err = w.Restore(&opts)
	s.ErrorIs(err, ErrRestoreWorktreeOnlyNotSupported)
}

func (s *WorktreeSuite) TestRestoreBoth() {
	_, w, names := setupForRestore(s)

	// Attempt without files should throw an error like the git restore --staged --worktree
	opts := RestoreOptions{Staged: true, Worktree: true}
	err := w.Restore(&opts)
	s.ErrorIs(err, ErrNoRestorePaths)

	// Restore Staged files in 2 groups and confirm status
	opts.Files = []string{names[0], names[1]}
	err = w.Restore(&opts)
	s.NoError(err)
	verifyStatus(s, "Restored First", w, names, []FileStatus{
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Modified, Staging: Modified},
		{Worktree: Unmodified, Staging: Deleted},
	})

	opts.Files = []string{names[2], names[3]}
	err = w.Restore(&opts)
	s.NoError(err)
	verifyStatus(s, "Restored Second", w, names, []FileStatus{
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
		{Worktree: Untracked, Staging: Untracked},
	})
}
