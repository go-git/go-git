package transport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const (
	refusedUnconfiguredMsg       = "By default, updating the current branch in a non-bare repository"
	refusedDeleteUnconfiguredMsg = "By default, deleting the current branch is denied"
)

// policyRepo is a repository whose refs/heads/main and refs/heads/other both
// point at tip, a child of base. HEAD is a symbolic reference to main.
type policyRepo struct {
	st        storage.Storer
	base, tip plumbing.Hash
}

// newPolicyRepo builds a policyRepo in memory and sets each "section.key"
// in config in its raw configuration. The repository is bare unless config
// sets core.bare.
func newPolicyRepo(t *testing.T, config map[string]string) policyRepo {
	t.Helper()
	return newPolicyRepoOn(t, memory.NewStorage(), config)
}

// newPolicyRepoOn is newPolicyRepo on st.
func newPolicyRepoOn(t *testing.T, st storage.Storer, config map[string]string) policyRepo {
	t.Helper()
	base := benchCommit(t, st, 1)
	tip := benchCommit(t, st, 2, base)
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/main", tip)))
	require.NoError(t, st.SetReference(plumbing.NewHashReference("refs/heads/other", tip)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

	cfg, err := st.Config()
	require.NoError(t, err)
	cfg.Core.IsBare = true
	if v, ok := config["core.bare"]; ok {
		if bare, err := parseConfigBool("core.bare", v); err == nil {
			cfg.Core.IsBare = bare
		}
	}
	for k, v := range config {
		section, key, ok := strings.Cut(k, ".")
		require.True(t, ok, k)
		cfg.Raw.Section(section).SetOption(key, v)
	}
	require.NoError(t, st.SetConfig(cfg))

	return policyRepo{st: st, base: base, tip: tip}
}

// push runs cmd through ReceivePack with side-band-64k and returns the
// report-status, the messages sent on the progress band and the error.
func (r policyRepo) push(t *testing.T, cmd *packp.Command) (report, messages string, err error) {
	t.Helper()
	var out bytes.Buffer
	err = ReceivePack(
		context.Background(), r.st,
		receivePackRequest(t, []*packp.Command{cmd}, capability.Sideband64k),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	p := readSideband(t, &out)
	return p.data.String(), p.progress.String(), err
}

func (r policyRepo) refHash(t *testing.T, name plumbing.ReferenceName) plumbing.Hash {
	t.Helper()
	ref, err := r.st.Reference(name)
	if err != nil {
		require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
		return plumbing.ZeroHash
	}
	return ref.Hash()
}

func TestReceivePackDenyCurrentBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   map[string]string
		ref      plumbing.ReferenceName
		detached bool
		reason   error
		messages []string
		absent   []string
	}{
		{
			name:   "non-bare refuses by default",
			config: map[string]string{"core.bare": "false"},
			reason: ErrBranchCheckedOut,
			messages: []string{
				"error: refusing to update checked out branch: refs/heads/main\n",
				"error: " + refusedUnconfiguredMsg,
			},
		},
		{
			name:     "refuse omits the advice",
			config:   map[string]string{"core.bare": "false", "receive.denyCurrentBranch": "refuse"},
			reason:   ErrBranchCheckedOut,
			messages: []string{"error: refusing to update checked out branch: refs/heads/main\n"},
			absent:   []string{refusedUnconfiguredMsg},
		},
		{
			name:     "true refuses",
			config:   map[string]string{"core.bare": "no", "receive.denyCurrentBranch": "true"},
			reason:   ErrBranchCheckedOut,
			messages: []string{"error: refusing to update checked out branch: refs/heads/main\n"},
			absent:   []string{refusedUnconfiguredMsg},
		},
		{
			name:   "ignore accepts",
			config: map[string]string{"core.bare": "false", "receive.denyCurrentBranch": "IGNORE"},
		},
		{
			name:   "false accepts",
			config: map[string]string{"core.bare": "false", "receive.denyCurrentBranch": "false"},
		},
		{
			name:     "warn accepts with a warning",
			config:   map[string]string{"core.bare": "false", "receive.denyCurrentBranch": "warn"},
			messages: []string{"warning: updating the current branch\n"},
		},
		{
			name:   "updateInstead is refused as unsupported",
			config: map[string]string{"core.bare": "false", "receive.denyCurrentBranch": "updateInstead"},
			reason: ErrUpdateInsteadUnsupported,
		},
		{
			name:   "bare repository accepts",
			config: map[string]string{"core.bare": "true"},
		},
		{
			name:   "other branch accepts",
			config: map[string]string{"core.bare": "false"},
			ref:    "refs/heads/other",
		},
		{
			name:     "detached HEAD accepts",
			config:   map[string]string{"core.bare": "false"},
			detached: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newPolicyRepo(t, tc.config)
			if tc.detached {
				require.NoError(t, repo.st.SetReference(plumbing.NewHashReference(plumbing.HEAD, repo.tip)))
			}
			ref := tc.ref
			if ref == "" {
				ref = "refs/heads/main"
			}

			report, messages, err := repo.push(t, &packp.Command{Name: ref, Old: repo.tip, New: repo.base})

			for _, m := range tc.messages {
				assert.Contains(t, messages, m)
			}
			for _, m := range tc.absent {
				assert.NotContains(t, messages, m)
			}
			if tc.reason != nil {
				require.ErrorIs(t, err, tc.reason)
				assert.Contains(t, report, "ng "+ref.String()+" "+tc.reason.Error()+"\n")
				assert.Equal(t, repo.tip, repo.refHash(t, ref))
				return
			}
			require.NoError(t, err)
			assert.Contains(t, report, "ok "+ref.String()+"\n")
			assert.Equal(t, repo.base, repo.refHash(t, ref))
		})
	}
}

func TestReceivePackCoreBare(t *testing.T) {
	t.Parallel()

	memoryRepo := func(raw string, isBare bool) func(t *testing.T) policyRepo {
		return func(t *testing.T) policyRepo {
			t.Helper()
			repo := newPolicyRepo(t, nil)
			cfg, err := repo.st.Config()
			require.NoError(t, err)
			cfg.Core.IsBare = isBare
			if raw != "" {
				cfg.Raw.Section("core").SetOption("bare", raw)
			}
			require.NoError(t, repo.st.SetConfig(cfg))
			return repo
		}
	}
	fileRepo := func(configFile string) func(t *testing.T) policyRepo {
		return func(t *testing.T) policyRepo {
			t.Helper()
			dir := t.TempDir()
			repo := newPolicyRepoOn(t, filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), nil)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config"), []byte(configFile), 0o644))
			return repo
		}
	}

	tests := []struct {
		name   string
		repo   func(t *testing.T) policyRepo
		reason error
	}{
		{
			name: "config file with a core section without bare accepts",
			repo: fileRepo("[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tlogallrefupdates = true\n"),
		},
		{
			name: "empty config file accepts",
			repo: fileRepo(""),
		},
		{
			name: "config file with bare true accepts",
			repo: fileRepo("[core]\n\tbare = true\n"),
		},
		{
			name:   "config file with bare false refuses",
			repo:   fileRepo("[core]\n\tbare = false\n"),
			reason: ErrBranchCheckedOut,
		},
		{
			name:   "memory repository with a work tree refuses",
			repo:   memoryRepo("", false),
			reason: ErrBranchCheckedOut,
		},
		{
			name: "memory bare repository accepts",
			repo: memoryRepo("", true),
		},
		{
			name: "raw bare true wins over Core.IsBare false",
			repo: memoryRepo("true", false),
		},
		{
			name:   "raw bare false wins over Core.IsBare true",
			repo:   memoryRepo("false", true),
			reason: ErrBranchCheckedOut,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := tc.repo(t)

			report, _, err := repo.push(t, &packp.Command{Name: "refs/heads/main", Old: repo.tip, New: repo.base})

			if tc.reason != nil {
				require.ErrorIs(t, err, tc.reason)
				assert.Contains(t, report, "ng refs/heads/main "+tc.reason.Error()+"\n")
				assert.Equal(t, repo.tip, repo.refHash(t, "refs/heads/main"))
				return
			}
			require.NoError(t, err)
			assert.Contains(t, report, "ok refs/heads/main\n")
			assert.Equal(t, repo.base, repo.refHash(t, "refs/heads/main"))
		})
	}
}

func TestReceivePackDenyCurrentBranchUnbornHead(t *testing.T) {
	t.Parallel()

	repo := newPolicyRepo(t, map[string]string{"core.bare": "false"})
	require.NoError(t, repo.st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/unborn")))

	report, _, err := repo.push(t, &packp.Command{Name: "refs/heads/unborn", Old: plumbing.ZeroHash, New: repo.tip})

	require.ErrorIs(t, err, ErrBranchCheckedOut)
	assert.Contains(t, report, "ng refs/heads/unborn branch is currently checked out\n")
	assert.Equal(t, plumbing.ZeroHash, repo.refHash(t, "refs/heads/unborn"))
}

// addLinkedWorktree records a linked worktree named name whose HEAD is
// head, laid out under .git/worktrees as git worktree add leaves it. Without
// a gitdir file git does not list the worktree.
func addLinkedWorktree(t *testing.T, gitDir, name, head string, withGitdir bool) {
	t.Helper()
	dir := filepath.Join(gitDir, "worktrees", name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "HEAD"), []byte(head+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "commondir"), []byte("../..\n"), 0o644))
	if withGitdir {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "gitdir"), []byte(filepath.Join(t.TempDir(), ".git")+"\n"), 0o644))
	}
}

func TestReceivePackProtectsLinkedWorktreeBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config map[string]string
		cmd    func(r policyRepo) *packp.Command
		reason error
	}{
		{
			name:   "bare repository refuses an update",
			config: map[string]string{"core.bare": "true"},
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/linked", Old: r.tip, New: r.base}
			},
			reason: ErrBranchCheckedOut,
		},
		{
			name:   "non-bare repository refuses an update",
			config: map[string]string{"core.bare": "false"},
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/linked", Old: r.tip, New: r.base}
			},
			reason: ErrBranchCheckedOut,
		},
		{
			name:   "bare repository refuses a delete",
			config: map[string]string{"core.bare": "true"},
			cmd: func(r policyRepo) *packp.Command {
				return deleteCmd("refs/heads/linked", r.tip)
			},
			reason: ErrBranchCheckedOut,
		},
		{
			name:   "denyCurrentBranch ignore still refuses a delete",
			config: map[string]string{"core.bare": "true", "receive.denyCurrentBranch": "ignore"},
			cmd: func(r policyRepo) *packp.Command {
				return deleteCmd("refs/heads/linked", r.tip)
			},
			reason: ErrDeleteCurrentBranch,
		},
		{
			name:   "denyCurrentBranch ignore accepts an update",
			config: map[string]string{"core.bare": "true", "receive.denyCurrentBranch": "ignore"},
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/linked", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "worktree without gitdir accepts an update",
			config: map[string]string{"core.bare": "true"},
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/stale", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "detached worktree accepts an update",
			config: map[string]string{"core.bare": "true"},
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: r.base}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			repo := newPolicyRepoOn(t, filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), tc.config)
			for _, name := range []plumbing.ReferenceName{"refs/heads/linked", "refs/heads/stale"} {
				require.NoError(t, repo.st.SetReference(plumbing.NewHashReference(name, repo.tip)))
			}
			addLinkedWorktree(t, dir, "linked", "ref: refs/heads/linked", true)
			addLinkedWorktree(t, dir, "stale", "ref: refs/heads/stale", false)
			addLinkedWorktree(t, dir, "detached", repo.tip.String(), true)
			cmd := tc.cmd(repo)

			report, _, err := repo.push(t, cmd)

			if tc.reason != nil {
				require.ErrorIs(t, err, tc.reason)
				assert.Contains(t, report, "ng "+cmd.Name.String()+" "+tc.reason.Error()+"\n")
				assert.Equal(t, repo.tip, repo.refHash(t, cmd.Name))
				return
			}
			require.NoError(t, err)
			assert.Contains(t, report, "ok "+cmd.Name.String()+"\n")
			assert.Equal(t, cmd.New, repo.refHash(t, cmd.Name))
		})
	}
}

func TestReceivePackDenyDeleteCurrent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   map[string]string
		ref      plumbing.ReferenceName
		reason   error
		messages []string
		absent   []string
	}{
		{
			name:   "non-bare reports the checked out branch first",
			config: map[string]string{"core.bare": "false"},
			reason: ErrBranchCheckedOut,
			absent: []string{refusedDeleteUnconfiguredMsg},
		},
		{
			name:   "non-bare with denyCurrentBranch ignore refuses by default",
			config: map[string]string{"core.bare": "false", "receive.denyCurrentBranch": "ignore"},
			reason: ErrDeleteCurrentBranch,
			messages: []string{
				"error: " + refusedDeleteUnconfiguredMsg,
				"error: refusing to delete the current branch: refs/heads/main\n",
			},
		},
		{
			name:   "bare refuses by default",
			config: map[string]string{"core.bare": "true"},
			reason: ErrDeleteCurrentBranch,
			messages: []string{
				"error: " + refusedDeleteUnconfiguredMsg,
				"error: refusing to delete the current branch: refs/heads/main\n",
			},
		},
		{
			name:     "refuse omits the advice",
			config:   map[string]string{"receive.denyDeleteCurrent": "refuse"},
			reason:   ErrDeleteCurrentBranch,
			messages: []string{"error: refusing to delete the current branch: refs/heads/main\n"},
			absent:   []string{refusedDeleteUnconfiguredMsg},
		},
		{
			name:     "updateInstead refuses",
			config:   map[string]string{"receive.denyDeleteCurrent": "updateInstead"},
			reason:   ErrDeleteCurrentBranch,
			messages: []string{"error: refusing to delete the current branch: refs/heads/main\n"},
			absent:   []string{refusedDeleteUnconfiguredMsg},
		},
		{
			name:     "warn accepts with a warning",
			config:   map[string]string{"receive.denyDeleteCurrent": "warn"},
			messages: []string{"warning: deleting the current branch\n"},
		},
		{
			name:   "ignore accepts",
			config: map[string]string{"receive.denyDeleteCurrent": "ignore"},
		},
		{
			name: "other branch accepts",
			ref:  "refs/heads/other",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newPolicyRepo(t, tc.config)
			ref := tc.ref
			if ref == "" {
				ref = "refs/heads/main"
			}

			report, messages, err := repo.push(t, deleteCmd(ref, repo.tip))

			for _, m := range tc.messages {
				assert.Contains(t, messages, m)
			}
			for _, m := range tc.absent {
				assert.NotContains(t, messages, m)
			}
			if tc.reason != nil {
				require.ErrorIs(t, err, tc.reason)
				assert.Contains(t, report, "ng "+ref.String()+" "+tc.reason.Error()+"\n")
				assert.Equal(t, repo.tip, repo.refHash(t, ref))
				return
			}
			require.NoError(t, err)
			assert.Contains(t, report, "ok "+ref.String()+"\n")
			assert.Equal(t, plumbing.ZeroHash, repo.refHash(t, ref))
		})
	}
}

func TestReceivePackDenyNonFastForwards(t *testing.T) {
	t.Parallel()

	deny := map[string]string{"core.bare": "true", "receive.denyNonFastForwards": "true"}

	tests := []struct {
		name     string
		config   map[string]string
		setup    func(t *testing.T, r policyRepo) *packp.Command
		reason   error
		messages []string
	}{
		{
			name:   "forced non-fast-forward is refused",
			config: deny,
			setup: func(_ *testing.T, r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: r.base}
			},
			reason:   ErrNonFastForward,
			messages: []string{"error: denying non-fast-forward refs/heads/other (you should pull first)\n"},
		},
		{
			name:   "forced non-fast-forward is accepted without the setting",
			config: map[string]string{"core.bare": "true"},
			setup: func(_ *testing.T, r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "forced non-fast-forward is accepted when the setting is false",
			config: map[string]string{"core.bare": "true", "receive.denyNonFastForwards": "off"},
			setup: func(_ *testing.T, r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "fast-forward is accepted",
			config: deny,
			setup: func(t *testing.T, r policyRepo) *packp.Command {
				child := benchCommit(t, r.st, 3, r.tip)
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: child}
			},
		},
		{
			name:   "forced tag update is accepted",
			config: deny,
			setup: func(t *testing.T, r policyRepo) *packp.Command {
				require.NoError(t, r.st.SetReference(plumbing.NewHashReference("refs/tags/v1", r.tip)))
				return &packp.Command{Name: "refs/tags/v1", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "create is accepted",
			config: deny,
			setup: func(_ *testing.T, r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/new", Old: plumbing.ZeroHash, New: r.base}
			},
		},
		{
			name:   "delete is accepted",
			config: deny,
			setup: func(_ *testing.T, r policyRepo) *packp.Command {
				return deleteCmd("refs/heads/other", r.tip)
			},
		},
		{
			name:   "new tip that is not a commit is a bad ref",
			config: deny,
			setup: func(t *testing.T, r policyRepo) *packp.Command {
				tag := tagObject(t, r.st, r.tip, plumbing.CommitObject)
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: tag}
			},
			reason: ErrBadRef,
		},
		{
			name:   "old tip that is not a commit is a bad ref",
			config: deny,
			setup: func(t *testing.T, r policyRepo) *packp.Command {
				tag := tagObject(t, r.st, r.base, plumbing.CommitObject)
				require.NoError(t, r.st.SetReference(plumbing.NewHashReference("refs/heads/other", tag)))
				return &packp.Command{Name: "refs/heads/other", Old: tag, New: r.tip}
			},
			reason: ErrBadRef,
		},
		{
			name:   "missing old tip is a bad ref",
			config: deny,
			setup: func(_ *testing.T, r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: plumbing.NewHash(receivePackTestHash), New: r.base}
			},
			reason: ErrBadRef,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newPolicyRepo(t, tc.config)
			cmd := tc.setup(t, repo)
			before := repo.refHash(t, cmd.Name)

			report, messages, err := repo.push(t, cmd)

			for _, m := range tc.messages {
				assert.Contains(t, messages, m)
			}
			if tc.reason != nil {
				require.ErrorIs(t, err, tc.reason)
				assert.Contains(t, report, "ng "+cmd.Name.String()+" "+tc.reason.Error()+"\n")
				assert.Equal(t, before, repo.refHash(t, cmd.Name))
				return
			}
			require.NoError(t, err)
			assert.Contains(t, report, "ok "+cmd.Name.String()+"\n")
			assert.Equal(t, cmd.New, repo.refHash(t, cmd.Name))
		})
	}
}

func TestReceivePackNonFastForwardWinsOverUpdateInstead(t *testing.T) {
	t.Parallel()

	repo := newPolicyRepo(t, map[string]string{
		"core.bare":                   "false",
		"receive.denyCurrentBranch":   "updateInstead",
		"receive.denyNonFastForwards": "true",
	})

	report, _, err := repo.push(t, &packp.Command{Name: "refs/heads/main", Old: repo.tip, New: repo.base})

	require.ErrorIs(t, err, ErrNonFastForward)
	assert.Contains(t, report, "ng refs/heads/main non-fast-forward\n")
}

func TestReceivePackReadsKeysWithoutValueAsTrue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		cmd    func(r policyRepo) *packp.Command
		reason error
		absent []string
	}{
		{
			name:   "denyCurrentBranch refuses",
			config: "[core]\n\tbare = false\n[receive]\n\tdenyCurrentBranch\n",
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/main", Old: r.tip, New: r.base}
			},
			reason: ErrBranchCheckedOut,
			absent: []string{refusedUnconfiguredMsg},
		},
		{
			name:   "denyCurrentBranch with an empty value ignores",
			config: "[core]\n\tbare = false\n[receive]\n\tdenyCurrentBranch =\n",
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/main", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "core.bare is bare",
			config: "[core]\n\tbare\n",
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/main", Old: r.tip, New: r.base}
			},
		},
		{
			name:   "core.bare with an empty value is not bare",
			config: "[core]\n\tbare =\n",
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/main", Old: r.tip, New: r.base}
			},
			reason: ErrBranchCheckedOut,
		},
		{
			name:   "denyDeleteCurrent refuses",
			config: "[receive]\n\tdenyDeleteCurrent\n",
			cmd: func(r policyRepo) *packp.Command {
				return deleteCmd("refs/heads/main", r.tip)
			},
			reason: ErrDeleteCurrentBranch,
			absent: []string{refusedDeleteUnconfiguredMsg},
		},
		{
			name:   "denyDeleteCurrent with an empty value ignores",
			config: "[receive]\n\tdenyDeleteCurrent =\n",
			cmd: func(r policyRepo) *packp.Command {
				return deleteCmd("refs/heads/main", r.tip)
			},
		},
		{
			name:   "denyNonFastForwards refuses",
			config: "[receive]\n\tdenyNonFastForwards\n",
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: r.base}
			},
			reason: ErrNonFastForward,
		},
		{
			name:   "denyNonFastForwards with an empty value accepts",
			config: "[receive]\n\tdenyNonFastForwards =\n",
			cmd: func(r policyRepo) *packp.Command {
				return &packp.Command{Name: "refs/heads/other", Old: r.tip, New: r.base}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newPolicyRepo(t, nil)
			cfg, err := repo.st.Config()
			require.NoError(t, err)
			require.NoError(t, format.NewDecoder(strings.NewReader(tc.config)).Decode(cfg.Raw))
			require.NoError(t, repo.st.SetConfig(cfg))
			cmd := tc.cmd(repo)
			before := repo.refHash(t, cmd.Name)

			report, messages, err := repo.push(t, cmd)

			for _, m := range tc.absent {
				assert.NotContains(t, messages, m)
			}
			if tc.reason != nil {
				require.ErrorIs(t, err, tc.reason)
				assert.Contains(t, report, "ng "+cmd.Name.String()+" "+tc.reason.Error()+"\n")
				assert.Equal(t, before, repo.refHash(t, cmd.Name))
				return
			}
			require.NoError(t, err)
			assert.Contains(t, report, "ok "+cmd.Name.String()+"\n")
			assert.Equal(t, cmd.New, repo.refHash(t, cmd.Name))
		})
	}
}

func TestReceivePackRejectsMalformedReceiveConfig(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"core.bare",
		"receive.denyCurrentBranch",
		"receive.denyDeleteCurrent",
		"receive.denyNonFastForwards",
	} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			repo := newPolicyRepo(t, map[string]string{key: "bogus"})

			var out bytes.Buffer
			err := ReceivePack(
				context.Background(), repo.st, nil,
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{AdvertiseRefs: true, StatelessRPC: true},
			)

			require.ErrorContains(t, err, "bad boolean config value 'bogus' for '"+strings.ToLower(key)+"'")
			assert.Empty(t, out.String())
		})
	}
}

func TestParseConfigBoolIntegers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
		bad   bool
	}{
		{value: "1", want: true},
		{value: "0"},
		{value: "00"},
		{value: "-1", want: true},
		{value: "+1", want: true},
		{value: " 1", want: true},
		{value: "010", want: true},
		{value: "0x1", want: true},
		{value: "0X0"},
		{value: " +0x10m", want: true},
		{value: "-0x80000000", want: true},
		{value: "0x7fffffff", want: true},
		{value: "1k", want: true},
		{value: "1K", want: true},
		{value: "0k"},
		{value: "0m"},
		{value: "1g", want: true},
		{value: "-2g", want: true},
		{value: "2097151k", want: true},
		{value: "-2097152k", want: true},
		{value: "2147483647", want: true},
		{value: "-2147483648", want: true},
		{value: "2147483648", bad: true},
		{value: "-2147483649", bad: true},
		{value: "0x80000000", bad: true},
		{value: "2097152k", bad: true},
		{value: "-2097153k", bad: true},
		{value: "2g", bad: true},
		{value: "9223372036854775808", bad: true},
		{value: "99999999999999999999", bad: true},
		{value: "08", bad: true},
		{value: "0x", bad: true},
		{value: "0xg", bad: true},
		{value: "1 ", bad: true},
		{value: "1kb", bad: true},
		{value: "--1", bad: true},
		{value: "+", bad: true},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			got, err := parseConfigBool("x.y", tc.value)
			if tc.bad {
				require.EqualError(t, err, "bad boolean config value '"+tc.value+"' for 'x.y'")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
