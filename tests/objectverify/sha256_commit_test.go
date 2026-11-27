//go:build linux

package objectverify_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

type gpgSigner struct {
	t *testing.T
}

func (s gpgSigner) Sign(_ context.Context, message io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(message)
	if err != nil {
		return nil, err
	}
	return []byte(gpgSign(s.t, payload)), nil
}

func TestSHA256CommitSignatureInterop(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping sha256 commit signature interop")
	}
	if gpg == nil {
		t.Error("gpg not available")
		t.FailNow()
	}
	if os.Getenv("GIT_VERSION") == "v2.11.0" {
		t.Skip("SHA-256 repositories need Git 2.29 or later")
	}

	t.Run("go-git signs, git verifies", func(t *testing.T) {
		t.Parallel()

		repo := initSHA256Repo(t)
		configureRepoSigning(t, repo, gpg.keyID)

		r, err := git.PlainOpen(repo)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		w, err := r.Worktree()
		require.NoError(t, err)

		hash, err := w.Commit("signed by go-git\n", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Author",
				Email: "author@example.local",
				When:  time.Unix(1700000000, 0).UTC(),
			},
			Signer:            gpgSigner{t: t},
			AllowEmptyCommits: true,
		})
		require.NoError(t, err)

		require.NoError(t, gitVerifyCommit(t, repo, hash))

		raw := readObjectBytes(t, repo, "commit", hash)
		tampered := writeLooseObject(t, repo, "commit",
			bytes.Replace(raw, []byte("\nsigned by go-git\n"), []byte("\ntampered\n"), 1))
		require.ErrorContains(t, gitVerifyCommit(t, repo, tampered), "BAD signature")
	})

	t.Run("git signs, go-git verifies", func(t *testing.T) {
		t.Parallel()

		repo := initSHA256Repo(t)
		configureRepoSigning(t, repo, gpg.keyID)
		tree := gitWriteEmptyTree(t, repo)
		hash := gitCommitTreeSigned(t, repo, tree, "signed by git\n", []string{
			"GIT_AUTHOR_DATE=1700000000 +0000",
			"GIT_COMMITTER_DATE=1700000000 +0000",
		})

		raw := readObjectBytes(t, repo, "commit", hash)
		require.Contains(t, string(raw), "\ngpgsig-sha256 ")
		tampered := writeLooseObject(t, repo, "commit",
			bytes.Replace(raw, []byte("\nsigned by git\n"), []byte("\ntampered\n"), 1))

		r, err := git.PlainOpen(repo)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		verify := func(h plumbing.Hash) error {
			c, err := r.CommitObject(h)
			require.NoError(t, err)
			_, err = c.Verify(gpg.pubKey)
			return err
		}

		require.NoError(t, verify(hash))
		require.ErrorContains(t, verify(tampered), "invalid signature")
	})
}

func initSHA256Repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out, err := gitenv.Command("git", "-C", dir, "init", "--quiet", "--object-format=sha256").CombinedOutput()
	require.NoErrorf(t, err, "git init: %s", out)
	return dir
}
