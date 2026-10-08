package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
)

// newFetchHeadSource builds, with git, a repository holding every kind of ref
// FETCH_HEAD describes differently: branches, a ref outside refs/heads and
// refs/tags, an annotated and a lightweight tag, and a tag that is not a
// commit.
func newFetchHeadSource(t *testing.T) string {
	t.Helper()

	src := t.TempDir()
	git(t, src, "init", "-q")
	git(t, src, "commit", "-q", "--allow-empty", "-m", "one")
	git(t, src, "tag", "-a", "-m", "v1", "v1")
	git(t, src, "tag", "light")
	git(t, src, "checkout", "-q", "-b", "feature")
	git(t, src, "commit", "-q", "--allow-empty", "-m", "two")
	git(t, src, "update-ref", "refs/pull/1/head", "HEAD")
	git(t, src, "checkout", "-q", "master")
	git(t, src, "commit", "-q", "--allow-empty", "-m", "three")

	require.NoError(t, os.WriteFile(filepath.Join(src, "blob"), []byte("blob\n"), 0o644))
	blob := strings.TrimSpace(git(t, src, "hash-object", "-w", "blob"))
	git(t, src, "tag", "blobtag", blob)

	git(t, src, "config", "uploadpack.allowReachableSHA1InWant", "true")
	return src
}

// newFetchHeadDestination makes an empty repository whose origin is src.
func newFetchHeadDestination(t *testing.T, src string) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "remote", "add", "origin", src)
	return dir
}

func readFetchHead(t *testing.T, dir string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, ".git", "FETCH_HEAD"))
	require.NoError(t, err)
	return string(b)
}

// TestFetchHeadMatchesGit fetches the same refs with git and with go-git into
// two identical repositories, and expects the same FETCH_HEAD from both.
//
// Cases that follow tags with explicit refspecs turn tag following off: git
// follows tags for any refspec with a destination, while go-git only does for
// wildcard refspecs, so the two fetch different tags to begin with.
func TestFetchHeadMatchesGit(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	src := newFetchHeadSource(t)
	feature := strings.TrimSpace(git(t, src, "rev-parse", "feature"))

	tests := []struct {
		name    string
		setup   []string
		gitArgs []string
		opts    FetchOptions
		wantErr bool
	}{{
		name:    "configured refspecs",
		gitArgs: []string{"fetch", "origin"},
	}, {
		name:    "already up to date",
		setup:   []string{"fetch -q origin"},
		gitArgs: []string{"fetch", "origin"},
	}, {
		name: "configured refspecs with an upstream",
		setup: []string{
			"config branch.master.remote origin",
			"config branch.master.merge refs/heads/feature",
		},
		gitArgs: []string{"fetch", "origin"},
	}, {
		name: "configured refspecs starting without a wildcard",
		setup: []string{
			"config remote.origin.fetch +refs/heads/master:refs/remotes/origin/master",
			"config --add remote.origin.fetch +refs/heads/*:refs/remotes/origin/*",
		},
		gitArgs: []string{"fetch", "--no-tags", "origin"},
		opts:    FetchOptions{Tags: plumbing.NoTags},
	}, {
		name:    "branch",
		gitArgs: []string{"fetch", "--no-tags", "origin", "master:refs/remotes/origin/master"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"master:refs/remotes/origin/master"},
			Tags:     plumbing.NoTags,
		},
	}, {
		name:    "two branches",
		gitArgs: []string{"fetch", "--no-tags", "origin", "master:refs/remotes/origin/master", "feature:refs/remotes/origin/feature"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"master:refs/remotes/origin/master", "feature:refs/remotes/origin/feature"},
			Tags:     plumbing.NoTags,
		},
	}, {
		name:    "wildcard",
		gitArgs: []string{"fetch", "origin", "+refs/heads/*:refs/remotes/origin/*"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
		},
	}, {
		name:    "ref outside refs/heads",
		gitArgs: []string{"fetch", "--no-tags", "origin", "refs/pull/1/head:refs/remotes/pull/1"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"refs/pull/1/head:refs/remotes/pull/1"},
			Tags:     plumbing.NoTags,
		},
	}, {
		name:    "HEAD",
		gitArgs: []string{"fetch", "--no-tags", "origin", "HEAD:refs/remotes/upstream/head"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"HEAD:refs/remotes/upstream/head"},
			Tags:     plumbing.NoTags,
		},
	}, {
		// git has no destination to name for an object, and go-git takes a
		// destination that is a hash as storing no reference.
		name:    "exact object name",
		gitArgs: []string{"fetch", "origin", feature},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{config.RefSpec("+" + feature + ":" + feature)},
		},
	}, {
		name:    "annotated tag",
		gitArgs: []string{"fetch", "--no-tags", "origin", "refs/tags/v1:refs/tags/v1"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"refs/tags/v1:refs/tags/v1"},
			Tags:     plumbing.NoTags,
		},
	}, {
		name:    "tag that is not a commit",
		gitArgs: []string{"fetch", "--no-tags", "origin", "refs/tags/blobtag:refs/tags/blobtag"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"refs/tags/blobtag:refs/tags/blobtag"},
			Tags:     plumbing.NoTags,
		},
	}, {
		name:    "all tags",
		gitArgs: []string{"fetch", "--tags", "origin", "master:refs/remotes/origin/master"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"master:refs/remotes/origin/master"},
			Tags:     plumbing.AllTags,
		},
	}, {
		name:    "append",
		setup:   []string{"fetch -q --no-tags origin feature:refs/remotes/origin/feature"},
		gitArgs: []string{"fetch", "--append", "--no-tags", "origin", "master:refs/remotes/origin/master"},
		opts: FetchOptions{
			RefSpecs:        []config.RefSpec{"master:refs/remotes/origin/master"},
			Tags:            plumbing.NoTags,
			AppendFetchHead: true,
		},
	}, {
		name: "rejected update",
		setup: []string{
			"commit -q --allow-empty -m unrelated",
			"update-ref refs/remotes/other/master HEAD",
		},
		gitArgs: []string{"fetch", "origin", "refs/heads/*:refs/remotes/other/*"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"refs/heads/*:refs/remotes/other/*"},
		},
		wantErr: true,
	}, {
		name:    "missing ref",
		setup:   []string{"fetch -q --no-tags origin feature:refs/remotes/origin/feature"},
		gitArgs: []string{"fetch", "--no-tags", "origin", "missing:refs/remotes/origin/missing"},
		opts: FetchOptions{
			RefSpecs: []config.RefSpec{"missing:refs/remotes/origin/missing"},
			Tags:     plumbing.NoTags,
		},
		wantErr: true,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			withGit := newFetchHeadDestination(t, src)
			withGoGit := newFetchHeadDestination(t, src)
			for _, dir := range []string{withGit, withGoGit} {
				for _, args := range tc.setup {
					git(t, dir, strings.Fields(args)...)
				}
			}

			out, ok := gitAllowFail(t, withGit, tc.gitArgs...)
			require.Equal(t, !tc.wantErr, ok, "git %v: %s", tc.gitArgs, out)

			r, err := PlainOpen(withGoGit)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			err = r.Fetch(&tc.opts)
			if tc.wantErr {
				require.Error(t, err)
			} else if !errors.Is(err, NoErrAlreadyUpToDate) {
				require.NoError(t, err)
			}

			want := readFetchHead(t, withGit)
			require.Equal(t, want, readFetchHead(t, withGoGit))
			if want == "" {
				return
			}

			// FETCH_HEAD names the object on its first line, to git and go-git.
			first := strings.Fields(want)[0]
			ref, err := r.Reference("FETCH_HEAD", false)
			require.NoError(t, err)
			assert.Equal(t, first, ref.Hash().String())
			assert.Equal(t, first, strings.TrimSpace(git(t, withGoGit, "rev-parse", "FETCH_HEAD")))
		})
	}
}

func TestFetchHeadCheckoutWithGit(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	src := newFetchHeadSource(t)
	dir := newFetchHeadDestination(t, src)

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	require.NoError(t, r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{"refs/pull/1/head:refs/remotes/pull/1"},
	}))

	want := strings.TrimSpace(git(t, src, "rev-parse", "refs/pull/1/head"))

	h, err := r.ResolveRevision("FETCH_HEAD")
	require.NoError(t, err)
	assert.Equal(t, want, h.String())

	git(t, dir, "checkout", "-q", "FETCH_HEAD")
	assert.Equal(t, want, strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")))
}

func TestFetchNoWriteFetchHead(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	src := newFetchHeadSource(t)
	dir := newFetchHeadDestination(t, src)
	fetchHead := filepath.Join(dir, ".git", "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fetchHead, []byte("untouched\n"), 0o644))

	r, err := PlainOpen(dir)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	require.NoError(t, r.Fetch(&FetchOptions{NoWriteFetchHead: true}))
	assert.Equal(t, "untouched\n", readFetchHead(t, dir))
}

func TestCloneDoesNotWriteFetchHead(t *testing.T) {
	t.Parallel()
	requireGitBinary(t)

	src := newFetchHeadSource(t)
	dir := t.TempDir()

	r, err := PlainClone(dir, &CloneOptions{URL: src})
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	_, err = os.Stat(filepath.Join(dir, ".git", "FETCH_HEAD"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFetchHeadURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url, want string
	}{
		{"https://github.com/go-git/go-git.git", "https://github.com/go-git/go-git"},
		{"https://github.com/go-git/go-git/", "https://github.com/go-git/go-git"},
		{"https://user:token@github.com/go-git/go-git.git", "https://github.com/go-git/go-git"},
		{"https://user@github.com/go-git/go-git", "https://github.com/go-git/go-git"},
		{"ssh://git@github.com/go-git/go-git.git", "ssh://github.com/go-git/go-git"},
		{"git@github.com:go-git/go-git.git", "github.com:go-git/go-git"},
		{"https://github.com/user@example.com/repo", "https://github.com/user@example.com/repo"},
		{"/srv/git/repo.git/", "/srv/git/repo"},
		{"/srv/git/user@host/repo", "/srv/git/user@host/repo"},
		{"/srv/git/repo/.git", "/srv/git/repo/"},
		{"a.git", "a.git"},
		{"ab.git", "ab"},
		{"/srv/git/new\nline", `/srv/git/new\nline`},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, fetchHeadURL(tc.url), "fetchHeadURL(%q)", tc.url)
	}
}
