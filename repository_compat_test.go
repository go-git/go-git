package git

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/server"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/compat"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

func TestCompatObjectFormat_FetchAndGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		localFixture         *fixtures.Fixture
		remoteFixture        *fixtures.Fixture
		objectFormat         formatcfg.ObjectFormat
		compatObjectFormat   formatcfg.ObjectFormat
		initialCommit        plumbing.Hash
		objectsAtFirstCommit int
		nativeTopCommit      plumbing.Hash
		compatTopCommit      plumbing.Hash
	}{
		{
			name:                 "sha256 repo fetches sha1 remote",
			localFixture:         fixtures.Basic().ByTag(".git").ByObjectFormat("sha256").One(),
			remoteFixture:        fixtures.Basic().ByTag(".git").ByObjectFormat("sha1").One(),
			objectFormat:         formatcfg.SHA256,
			compatObjectFormat:   formatcfg.SHA1,
			initialCommit:        plumbing.NewHash("9768a9bcb42f35dc598a517bd98a5cbba79052b980a8a015f3be5577ebd9f201"),
			nativeTopCommit:      plumbing.NewHash("4fef4adac3be863b9b94613016bdd8e53f67f6d7577234e028bc9d24c5a6a27c"),
			compatTopCommit:      plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			objectsAtFirstCommit: 4,
		},
		{
			name:                 "sha1 repo fetches sha256 remote",
			localFixture:         fixtures.Basic().ByTag(".git").ByObjectFormat("sha1").One(),
			remoteFixture:        fixtures.Basic().ByTag(".git").ByObjectFormat("sha256").One(),
			objectFormat:         formatcfg.SHA1,
			compatObjectFormat:   formatcfg.SHA256,
			initialCommit:        plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
			nativeTopCommit:      plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			compatTopCommit:      plumbing.NewHash("4fef4adac3be863b9b94613016bdd8e53f67f6d7577234e028bc9d24c5a6a27c"),
			objectsAtFirstCommit: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, tc.localFixture, "local Basic fixture not found")
			require.NotNil(t, tc.remoteFixture, "remote Basic fixture not found")
			require.NotEqual(t, tc.objectFormat, tc.compatObjectFormat)
			assert.Equal(t, string(tc.objectFormat), tc.localFixture.ObjectFormat)
			assert.Equal(t, string(tc.compatObjectFormat), tc.remoteFixture.ObjectFormat)

			servers := server.All(server.Loader(t, tc.remoteFixture))
			require.NotEmpty(t, servers)

			for _, srv := range servers {
				endpoint, err := srv.Start()
				require.NoError(t, err)

				t.Cleanup(func() {
					require.NoError(t, srv.Close())
				})

				st := memory.NewStorage(
					memory.WithObjectFormat(tc.objectFormat),
					memory.WithCompatObjectFormat(tc.compatObjectFormat),
				)
				r, err := Init(st, WithObjectFormat(tc.objectFormat))
				require.NoError(t, err)

				cfg, err := r.Config()
				require.NoError(t, err)
				assert.Equal(t, tc.objectFormat, cfg.Extensions.ObjectFormat)
				assert.Equal(t, tc.compatObjectFormat, cfg.Extensions.CompatObjectFormat)

				copyReachableObjects(t, st, tc.localFixture, tc.initialCommit)
				require.NoError(t, r.Storer.SetReference(plumbing.NewHashReference(plumbing.Master, tc.initialCommit)))
				assert.Len(t, st.Objects, tc.objectsAtFirstCommit)

				_, err = r.CreateRemote(&config.RemoteConfig{
					Name: DefaultRemoteName,
					URLs: []string{endpoint},
				})
				require.NoError(t, err)

				err = r.Fetch(&FetchOptions{})
				require.NoError(t, err)

				ref, err := r.Reference(plumbing.NewRemoteReferenceName(DefaultRemoteName, "master"), true)
				require.NoError(t, err)
				assert.Equal(t, tc.nativeTopCommit, ref.Hash())

				nativeCommit, err := r.Object(plumbing.CommitObject, tc.nativeTopCommit)
				require.NoError(t, err, "top commit must be reachable by native OID %s", tc.nativeTopCommit)

				compatCommit, err := r.Object(plumbing.CommitObject, tc.compatTopCommit)
				require.NoError(t, err, "top commit must be reachable by compat OID %s", tc.compatTopCommit)

				assert.Equal(t, nativeCommit.Type(), compatCommit.Type())
				assert.Equal(t, nativeCommit.ID(), tc.nativeTopCommit)
				// The compatCommit is the native objectFormat representation, which
				// makes sense, but still represents an awkward UX:
				// - Using the SHA256 ID returns the SHA1 version of the encoded object.
				assert.Equal(t, compatCommit.ID(), tc.nativeTopCommit)
			}
		})
	}
}

func copyReachableObjects(t *testing.T, dst *memory.Storage, src *fixtures.Fixture, want plumbing.Hash) {
	t.Helper()

	dotgit, err := src.DotGit(fixtures.WithMemFS())
	require.NoError(t, err)

	srcStorage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	hashes, err := revlist.Objects(srcStorage, []plumbing.Hash{want}, nil)
	require.NoError(t, err)

	for _, h := range hashes {
		obj, err := srcStorage.EncodedObject(plumbing.AnyObject, h)
		require.NoError(t, err)

		copied := dst.NewEncodedObject()
		copied.SetType(obj.Type())
		copied.SetSize(obj.Size())

		reader, err := obj.Reader()
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())

		writer, err := copied.Writer()
		require.NoError(t, err)
		_, err = writer.Write(content)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		_, err = dst.SetEncodedObject(copied)
		require.NoError(t, err)
	}

	require.NoError(t, compat.TranslateStoredObjects(dst, dst.Translator()))
}

func TestCompatInteropValidationMatrix(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	t.Run("native sha1 open repack reopen", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		repoDir := filepath.Join(root, "repo-sha1")

		headHash := initUpstreamRepoWithHistory(t, repoDir, "sha1")
		reopenAndValidateNativeRepo(t, repoDir, formatcfg.SHA1, headHash)
	})

	t.Run("native sha256 open repack reopen", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		repoDir := filepath.Join(root, "repo-sha256")

		headHash := initUpstreamRepoWithHistory(t, repoDir, "sha256")
		reopenAndValidateNativeRepo(t, repoDir, formatcfg.SHA256, headHash)
	})

	t.Run("compat fetch sha1 native remote with legacy mapping writes", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-sha1")
		localDir := filepath.Join(root, "local-compat-sha1")

		headHash := initUpstreamRepoWithHistory(t, remoteDir, "sha1")
		validateCompatFetchLifecycle(t, compatFetchCase{
			localDir:             localDir,
			remoteDir:            remoteDir,
			remoteHeadHash:       headHash,
			remoteObjectFormat:   formatcfg.SHA1,
			nativeFormat:         formatcfg.SHA1,
			compatFormat:         formatcfg.SHA256,
			enableObjectMapWrite: false,
			expectMappingPath:    filepath.Join(localDir, GitDirName, "objects", "loose-object-idx"),
		})
	})

	t.Run("compat fetch sha1 remote into sha256 native repo", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-sha1-to-sha256")
		localDir := filepath.Join(root, "local-compat-sha256-native")

		headHash := initUpstreamRepoWithHistory(t, remoteDir, "sha1")
		validateCompatFetchLifecycle(t, compatFetchCase{
			localDir:             localDir,
			remoteDir:            remoteDir,
			remoteHeadHash:       headHash,
			remoteObjectFormat:   formatcfg.SHA1,
			nativeFormat:         formatcfg.SHA256,
			compatFormat:         formatcfg.SHA1,
			enableObjectMapWrite: true,
			expectMappingPath:    filepath.Join(localDir, GitDirName, "objects", "object-map"),
		})
	})

	t.Run("compat fetch sha256 native remote with object-map writes", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-sha256")
		localDir := filepath.Join(root, "local-compat-sha256")

		headHash := initUpstreamRepoWithHistory(t, remoteDir, "sha256")
		validateCompatFetchLifecycle(t, compatFetchCase{
			localDir:             localDir,
			remoteDir:            remoteDir,
			remoteHeadHash:       headHash,
			remoteObjectFormat:   formatcfg.SHA256,
			nativeFormat:         formatcfg.SHA256,
			compatFormat:         formatcfg.SHA1,
			enableObjectMapWrite: true,
			expectMappingPath:    filepath.Join(localDir, GitDirName, "objects", "object-map"),
		})
	})

	t.Run("compat fetch sha256 remote into sha1 native repo", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-sha256-to-sha1")
		localDir := filepath.Join(root, "local-compat-sha1-native")

		headHash := initUpstreamRepoWithHistory(t, remoteDir, "sha256")
		validateCompatFetchLifecycle(t, compatFetchCase{
			localDir:             localDir,
			remoteDir:            remoteDir,
			remoteHeadHash:       headHash,
			remoteObjectFormat:   formatcfg.SHA256,
			nativeFormat:         formatcfg.SHA1,
			compatFormat:         formatcfg.SHA256,
			enableObjectMapWrite: false,
			expectMappingPath:    filepath.Join(localDir, GitDirName, "objects", "loose-object-idx"),
		})
	})

	t.Run("compat incremental fetch preserves old and new mappings", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-incremental")
		localDir := filepath.Join(root, "local-compat-incremental")

		firstHead := initUpstreamRepoWithHistory(t, remoteDir, "sha1")
		r := initCompatFetchRepo(t, localDir, remoteDir, formatcfg.SHA1, formatcfg.SHA256, false)

		require.NoError(t, r.Fetch(&FetchOptions{}))
		validateCompatLookup(t, r, firstHead)

		secondHead := appendUpstreamCommit(t, remoteDir, "gamma.txt", "gamma\n", "third")
		require.NoError(t, r.Fetch(&FetchOptions{}))

		ref, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), true)
		require.NoError(t, err)
		assert.Equal(t, secondHead, ref.Hash())

		validateCompatLookup(t, r, firstHead)
		validateCompatLookup(t, r, secondHead)

		reopened, err := openCompatRepo(localDir, false)
		require.NoError(t, err)
		validateCompatLookup(t, reopened, firstHead)
		validateCompatLookup(t, reopened, secondHead)
	})

	t.Run("compat repo survives upstream git gc lifecycle", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-gc")
		localDir := filepath.Join(root, "local-compat-gc")

		headHash := initUpstreamRepoWithHistory(t, remoteDir, "sha256")
		r := initCompatFetchRepo(t, localDir, remoteDir, formatcfg.SHA256, formatcfg.SHA1, true)

		require.NoError(t, r.Fetch(&FetchOptions{}))
		validateCompatLookup(t, r, headHash)

		mappingDir := filepath.Join(localDir, GitDirName, "objects", "object-map")
		entriesBefore, err := os.ReadDir(mappingDir)
		require.NoError(t, err)
		require.NotEmpty(t, entriesBefore)

		mustRunGitCmd(t, localDir, nil, "git", "gc")

		reopened, err := openCompatRepo(localDir, true)
		require.NoError(t, err)

		ref, err := reopened.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), true)
		require.NoError(t, err)
		assert.Equal(t, headHash, ref.Hash())

		validateCompatLookup(t, reopened, headHash)

		entriesAfter, err := os.ReadDir(mappingDir)
		require.NoError(t, err)
		require.NotEmpty(t, entriesAfter)
	})

	t.Run("compat lookup survives alternates-only object access", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-alternate")
		localDir := filepath.Join(root, "local-compat-alternate")

		headHash := initUpstreamRepoWithHistory(t, remoteDir, "sha256")
		r := initCompatFetchRepo(t, localDir, remoteDir, formatcfg.SHA256, formatcfg.SHA1, true)

		require.NoError(t, r.Fetch(&FetchOptions{}))
		validateCompatLookup(t, r, headHash)

		require.NoError(t, r.Storer.AddAlternate(filepath.Join(remoteDir, GitDirName)))
		require.NoError(t, stripLocalObjectDataPreservingCompatMetadata(filepath.Join(localDir, GitDirName, "objects")))

		reopened, err := openCompatRepo(localDir, true)
		require.NoError(t, err)

		ref, err := reopened.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), true)
		require.NoError(t, err)
		assert.Equal(t, headHash, ref.Hash())

		validateCompatLookup(t, reopened, headHash)
	})

	t.Run("compat push creates sha1 remote from sha256 local", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push.git")
		localDir := filepath.Join(root, "local-push")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		localHead := commitCompatPushFile(t, w, localDir, "push\n", "push commit")
		assertCompatPushCommitReadable(t, r, localHead)

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		compatHead := compatHeadForRepo(t, r, localHead)
		remoteHead := remoteHeadHash(t, remoteDir)
		assert.Equal(t, compatHead.String(), remoteHead)
		assert.Len(t, remoteHead, formatcfg.SHA1HexSize)

		remoteCommit := mustRunGitCmd(t, "", nil, "git", "--git-dir", remoteDir, "cat-file", "-p", "refs/heads/main")
		assert.Contains(t, remoteCommit, "tree ")
		assert.Contains(t, remoteCommit, "push commit")
	})

	t.Run("compat push fast-forwards sha1 remote from sha256 local", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-ff.git")
		localDir := filepath.Join(root, "local-push-ff")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		firstHead := commitCompatPushFile(t, w, localDir, "first\n", "first push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		secondHead := commitCompatPushFile(t, w, localDir, "second\n", "second push")

		err = r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		firstCompat := compatHeadForRepo(t, r, firstHead)
		secondCompat := compatHeadForRepo(t, r, secondHead)
		assert.NotEqual(t, firstCompat, secondCompat)

		remoteHead := remoteHeadHash(t, remoteDir)
		assert.Equal(t, secondCompat.String(), remoteHead)

		remoteCommit := mustRunGitCmd(t, "", nil, "git", "--git-dir", remoteDir, "cat-file", "-p", "refs/heads/main")
		assert.Contains(t, remoteCommit, "second push")
	})

	t.Run("compat push no-op reports already up to date", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-noop.git")
		localDir := filepath.Join(root, "local-push-noop")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		localHead := commitCompatPushFile(t, w, localDir, "noop\n", "noop push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		err = r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.ErrorIs(t, err, NoErrAlreadyUpToDate)

		compatHead := compatHeadForRepo(t, r, localHead)
		remoteHead := remoteHeadHash(t, remoteDir)
		assert.Equal(t, compatHead.String(), remoteHead)
	})

	t.Run("compat push deletes remote ref", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-delete.git")
		localDir := filepath.Join(root, "local-push-delete")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		_ = commitCompatPushFile(t, w, localDir, "delete\n", "delete push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		err = r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{":refs/heads/main"},
		})
		require.NoError(t, err)

		_, err = runGitCmd("", nil, "git", "--git-dir", remoteDir, "rev-parse", "--verify", "refs/heads/main")
		require.Error(t, err)
	})

	t.Run("compat push rejects non-fast-forward update", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-nff.git")
		localDir := filepath.Join(root, "local-push-nff")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		_ = commitCompatPushFile(t, w, localDir, "base\n", "base push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		remoteHeadBefore := remoteHeadHash(t, remoteDir)
		remoteHeadAfterAdvance := advanceBareRemoteMain(t, root, remoteDir, "remote-advance", "remote\n", "remote advance")
		assert.NotEqual(t, remoteHeadBefore, remoteHeadAfterAdvance)

		_ = commitCompatPushFile(t, w, localDir, "local divergence\n", "local divergence")

		err = r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "non-fast-forward update")

		remoteHeadFinal := remoteHeadHash(t, remoteDir)
		assert.Equal(t, remoteHeadAfterAdvance, remoteHeadFinal)
	})

	t.Run("compat push force updates divergent remote", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-force.git")
		localDir := filepath.Join(root, "local-push-force")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		_ = commitCompatPushFile(t, w, localDir, "base\n", "base push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		_ = advanceBareRemoteMain(t, root, remoteDir, "remote-force-advance", "remote\n", "remote advance")
		localHead := commitCompatPushFile(t, w, localDir, "forced local\n", "forced local divergence")

		err = r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
			Force:      true,
		})
		require.NoError(t, err)

		compatHead := compatHeadForRepo(t, r, localHead)
		remoteHead := remoteHeadHash(t, remoteDir)
		assert.Equal(t, compatHead.String(), remoteHead)
	})

	t.Run("compat push force-with-lease rejects stale tracking ref", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-lease-fail.git")
		localDir := filepath.Join(root, "local-push-lease-fail")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		_ = commitCompatPushFile(t, w, localDir, "base\n", "base push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		_ = advanceBareRemoteMain(t, root, remoteDir, "remote-lease-fail-advance", "remote\n", "remote advance")
		_ = commitCompatPushFile(t, w, localDir, "lease fail local\n", "lease fail local divergence")

		err = r.Push(&PushOptions{
			RemoteName:     DefaultRemoteName,
			RefSpecs:       []config.RefSpec{"refs/heads/main:refs/heads/main"},
			ForceWithLease: &ForceWithLease{},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "non-fast-forward update")
	})

	t.Run("compat push force-with-lease succeeds with refreshed tracking ref", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		remoteDir := filepath.Join(root, "remote-push-lease-ok.git")
		localDir := filepath.Join(root, "local-push-lease-ok")

		r, w := initCompatPushRepo(t, localDir, remoteDir)
		_ = commitCompatPushFile(t, w, localDir, "base\n", "base push")

		err := r.Push(&PushOptions{
			RemoteName: DefaultRemoteName,
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		require.NoError(t, err)

		remoteTrackingHash := plumbing.NewHash(advanceBareRemoteMain(t, root, remoteDir, "remote-lease-ok-advance", "remote\n", "remote advance"))
		require.NoError(t, r.Fetch(&FetchOptions{}))
		ref, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), true)
		require.NoError(t, err)
		provider, ok := r.Storer.(xstorage.CompatTranslatorProvider)
		require.True(t, ok)
		nativeTrackingHash, err := provider.Translator().Mapping().ToNative(remoteTrackingHash)
		require.NoError(t, err)
		assert.Equal(t, nativeTrackingHash, ref.Hash())

		localHead := commitCompatPushFile(t, w, localDir, "lease ok local\n", "lease ok local divergence")

		err = r.Push(&PushOptions{
			RemoteName:     DefaultRemoteName,
			RefSpecs:       []config.RefSpec{"refs/heads/main:refs/heads/main"},
			ForceWithLease: &ForceWithLease{},
		})
		require.NoError(t, err)

		compatHead := compatHeadForRepo(t, r, localHead)
		remoteHead := remoteHeadHash(t, remoteDir)
		assert.Equal(t, compatHead.String(), remoteHead)
	})
}

func initCompatPushRepo(t *testing.T, localDir, remoteDir string) (*Repository, *Worktree) {
	t.Helper()

	initUpstreamBareRepo(t, remoteDir, "sha1")

	r, err := PlainInit(localDir, false,
		WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
		WithObjectFormat(formatcfg.SHA256),
	)
	require.NoError(t, err)

	cfg, err := r.Config()
	require.NoError(t, err)
	cfg.Core.RepositoryFormatVersion = formatcfg.Version1
	cfg.Extensions.ObjectFormat = formatcfg.SHA256
	cfg.Extensions.CompatObjectFormat = formatcfg.SHA1
	require.NoError(t, r.SetConfig(cfg))

	r, err = PlainOpen(localDir)
	require.NoError(t, err)

	_, err = r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{remoteDir},
	})
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	return r, w
}

func commitCompatPushFile(t *testing.T, w *Worktree, localDir, content, message string) plumbing.Hash {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(localDir, "push.txt"), []byte(content), 0o644))
	_, err := w.Add("push.txt")
	require.NoError(t, err)

	head, err := w.Commit(message, &CommitOptions{Author: defaultSignature()})
	require.NoError(t, err)
	return head
}

func assertCompatPushCommitReadable(t *testing.T, r *Repository, h plumbing.Hash) {
	t.Helper()

	_, err := r.Storer.EncodedObject(plumbing.AnyObject, h)
	require.NoError(t, err)
	_, err = r.Storer.EncodedObject(plumbing.CommitObject, h)
	require.NoError(t, err)
	_, err = revlist.Objects(r.Storer, []plumbing.Hash{h}, nil)
	require.NoError(t, err)
}

func compatHeadForRepo(t *testing.T, r *Repository, native plumbing.Hash) plumbing.Hash {
	t.Helper()

	provider, ok := r.Storer.(xstorage.CompatTranslatorProvider)
	require.True(t, ok)

	compatHead, err := provider.Translator().Mapping().ToCompat(native)
	require.NoError(t, err)
	return compatHead
}

func remoteHeadHash(t *testing.T, remoteDir string) string {
	t.Helper()
	return strings.TrimSpace(mustRunGitCmd(t, "", nil, "git", "--git-dir", remoteDir, "rev-parse", "refs/heads/main"))
}

func compatE2EGitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=Compat E2E",
		"GIT_AUTHOR_EMAIL=compat-e2e@example.com",
		"GIT_COMMITTER_NAME=Compat E2E",
		"GIT_COMMITTER_EMAIL=compat-e2e@example.com",
	}
}

func advanceBareRemoteMain(t *testing.T, root, remoteDir, cloneDirName, content, message string) string {
	t.Helper()

	cloneDir := filepath.Join(root, cloneDirName)
	mustRunGitCmd(t, "", nil, "git", "clone", "--branch", "main", remoteDir, cloneDir)
	require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "remote.txt"), []byte(content), 0o644))
	env := compatE2EGitEnv()
	mustRunGitCmd(t, cloneDir, env, "git", "add", "remote.txt")
	mustRunGitCmd(t, cloneDir, env, "git", "commit", "-m", message)
	mustRunGitCmd(t, cloneDir, env, "git", "push", "origin", "main")
	return remoteHeadHash(t, remoteDir)
}

type compatFetchCase struct {
	localDir             string
	remoteDir            string
	remoteHeadHash       plumbing.Hash
	remoteObjectFormat   formatcfg.ObjectFormat
	nativeFormat         formatcfg.ObjectFormat
	compatFormat         formatcfg.ObjectFormat
	enableObjectMapWrite bool
	expectMappingPath    string
}

func validateCompatFetchLifecycle(t *testing.T, tc compatFetchCase) {
	t.Helper()

	r := initCompatFetchRepo(t, tc.localDir, tc.remoteDir, tc.nativeFormat, tc.compatFormat, tc.enableObjectMapWrite)

	require.NoError(t, r.Fetch(&FetchOptions{}))

	expectedHead := tc.remoteHeadHash
	if tc.remoteObjectFormat != tc.nativeFormat {
		provider, ok := r.Storer.(xstorage.CompatTranslatorProvider)
		require.True(t, ok)

		var err error
		expectedHead, err = provider.Translator().Mapping().ToNative(tc.remoteHeadHash)
		require.NoError(t, err)
	}

	ref, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), true)
	require.NoError(t, err)
	assert.Equal(t, expectedHead, ref.Hash())

	validateCompatLookup(t, r, expectedHead)

	_, err = os.Stat(tc.expectMappingPath)
	require.NoError(t, err)

	require.NoError(t, r.RepackObjects(&RepackConfig{}))

	reopened, err := openCompatRepo(tc.localDir, tc.enableObjectMapWrite)
	require.NoError(t, err)

	reopenedRef, err := reopened.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), true)
	require.NoError(t, err)
	assert.Equal(t, expectedHead, reopenedRef.Hash())

	validateCompatLookup(t, reopened, expectedHead)
}

func initCompatFetchRepo(
	t *testing.T,
	localDir, remoteDir string,
	nativeFormat, compatFormat formatcfg.ObjectFormat,
	enableObjectMapWrite bool,
) *Repository {
	t.Helper()

	initOpts := []InitOption{WithObjectFormat(nativeFormat)}
	if enableObjectMapWrite {
		initOpts = append(initOpts, WithCompatObjectMapWrite(true))
	}

	r, err := PlainInit(localDir, false, initOpts...)
	require.NoError(t, err)

	cfg, err := r.Config()
	require.NoError(t, err)
	cfg.Core.RepositoryFormatVersion = formatcfg.Version1
	cfg.Extensions.ObjectFormat = nativeFormat
	cfg.Extensions.CompatObjectFormat = compatFormat
	require.NoError(t, r.SetConfig(cfg))

	r, err = openCompatRepo(localDir, enableObjectMapWrite)
	require.NoError(t, err)

	_, err = r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{remoteDir},
	})
	require.NoError(t, err)

	return r
}

func validateCompatLookup(t *testing.T, r *Repository, nativeHash plumbing.Hash) {
	t.Helper()

	provider, ok := r.Storer.(xstorage.CompatTranslatorProvider)
	require.True(t, ok)

	translator := provider.Translator()
	require.NotNil(t, translator)

	compatHash, err := translator.Mapping().ToCompat(nativeHash)
	require.NoError(t, err)

	obj, err := r.Storer.EncodedObject(plumbing.CommitObject, compatHash)
	require.NoError(t, err)
	assert.Equal(t, nativeHash, obj.Hash())
}

func reopenAndValidateNativeRepo(t *testing.T, repoDir string, format formatcfg.ObjectFormat, headHash plumbing.Hash) {
	t.Helper()

	r, err := PlainOpen(repoDir)
	require.NoError(t, err)

	cfg, err := r.Config()
	require.NoError(t, err)
	if format == formatcfg.SHA1 {
		assert.Contains(t, []formatcfg.ObjectFormat{formatcfg.UnsetObjectFormat, formatcfg.SHA1}, cfg.Extensions.ObjectFormat)
	} else {
		assert.Equal(t, format, cfg.Extensions.ObjectFormat)
	}

	head, err := r.Head()
	require.NoError(t, err)
	assert.Equal(t, headHash, head.Hash())

	require.NoError(t, r.RepackObjects(&RepackConfig{}))

	reopened, err := PlainOpen(repoDir)
	require.NoError(t, err)

	reopenedHead, err := reopened.Head()
	require.NoError(t, err)
	assert.Equal(t, headHash, reopenedHead.Hash())
}

func openCompatRepo(path string, enableObjectMapWrite bool) (*Repository, error) {
	if !enableObjectMapWrite {
		return PlainOpen(path)
	}
	return PlainOpenWithOptions(path, &PlainOpenOptions{EnableCompatObjectMapWrite: true})
}

func initUpstreamRepoWithHistory(t *testing.T, dir, objectFormat string) plumbing.Hash {
	t.Helper()

	args := []string{"init"}
	if objectFormat != "sha1" {
		args = append(args, "--object-format="+objectFormat)
	}
	args = append(args, dir)

	out, err := runGitCmd("", nil, "git", args...)
	if err != nil {
		if strings.Contains(out, "unknown option") && strings.Contains(out, "object-format") {
			t.Skip("installed git does not support --object-format")
		}
		if objectFormat == "sha256" && strings.Contains(strings.ToLower(out), "sha256") {
			t.Skipf("installed git does not support sha256 repositories: %s", out)
		}
		require.NoError(t, err, out)
	}

	env := []string{
		"GIT_AUTHOR_NAME=Compat E2E",
		"GIT_AUTHOR_EMAIL=compat-e2e@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=Compat E2E",
		"GIT_COMMITTER_EMAIL=compat-e2e@example.com",
		"GIT_COMMITTER_DATE=1700000000 +0000",
	}

	mustRunGitCmd(t, dir, env, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("alpha\n"), 0o644))
	mustRunGitCmd(t, dir, env, "git", "add", "alpha.txt")
	mustRunGitCmd(t, dir, env, "git", "commit", "-m", "first")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("alpha\nbeta\n"), 0o644))
	mustRunGitCmd(t, dir, env, "git", "add", "alpha.txt")
	mustRunGitCmd(t, dir, env, "git", "commit", "-m", "second")

	return plumbing.NewHash(strings.TrimSpace(mustRunGitCmd(t, dir, env, "git", "rev-parse", "HEAD")))
}

func initUpstreamBareRepo(t *testing.T, dir, objectFormat string) {
	t.Helper()

	args := []string{"init", "--bare"}
	if objectFormat != "sha1" {
		args = append(args, "--object-format="+objectFormat)
	}
	args = append(args, dir)

	out, err := runGitCmd("", nil, "git", args...)
	if err != nil {
		if strings.Contains(out, "unknown option") && strings.Contains(out, "object-format") {
			t.Skip("installed git does not support --object-format")
		}
		if objectFormat == "sha256" && strings.Contains(strings.ToLower(out), "sha256") {
			t.Skipf("installed git does not support sha256 repositories: %s", out)
		}
		require.NoError(t, err, out)
	}
}

func appendUpstreamCommit(t *testing.T, dir, file, content, message string) plumbing.Hash {
	t.Helper()

	env := []string{
		"GIT_AUTHOR_NAME=Compat E2E",
		"GIT_AUTHOR_EMAIL=compat-e2e@example.com",
		"GIT_AUTHOR_DATE=1700000100 +0000",
		"GIT_COMMITTER_NAME=Compat E2E",
		"GIT_COMMITTER_EMAIL=compat-e2e@example.com",
		"GIT_COMMITTER_DATE=1700000100 +0000",
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
	mustRunGitCmd(t, dir, env, "git", "add", file)
	mustRunGitCmd(t, dir, env, "git", "commit", "-m", message)

	return plumbing.NewHash(strings.TrimSpace(mustRunGitCmd(t, dir, env, "git", "rev-parse", "HEAD")))
}

func stripLocalObjectDataPreservingCompatMetadata(objectsDir string) error {
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.IsDir() && name == "info":
			continue
		case entry.IsDir() && name == "object-map":
			continue
		case entry.IsDir() && len(name) == 2:
			if err := os.RemoveAll(filepath.Join(objectsDir, name)); err != nil {
				return err
			}
		case entry.IsDir() && name == "pack":
			packEntries, err := os.ReadDir(filepath.Join(objectsDir, name))
			if err != nil {
				return err
			}
			for _, packEntry := range packEntries {
				if err := os.RemoveAll(filepath.Join(objectsDir, name, packEntry.Name())); err != nil {
					return err
				}
			}
		case !entry.IsDir() && name == "loose-object-idx":
			continue
		}
	}

	return nil
}

func mustRunGitCmd(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	out, err := runGitCmd(dir, env, name, args...)
	if err != nil && strings.Contains(err.Error(), "compatibility hash algorithm support requires Rust") {
		t.Skip("installed git does not support compatibility hash algorithms without Rust")
	}
	require.NoError(t, err, out)
	return strings.TrimSpace(out)
}

func runGitCmd(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String() + stderr.String(), fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}
