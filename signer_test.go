package git

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/x/plugin"
	xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

type b64signer struct{}

// This is not secure, and is only used as an example for testing purposes.
// Please don't do this.
func (b64signer) Sign(_ context.Context, message io.Reader) ([]byte, error) {
	b, err := io.ReadAll(message)
	if err != nil {
		return nil, err
	}
	out := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(out, b)
	return out, nil
}

func ExampleSigner() {
	repo, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	if err != nil {
		panic(err)
	}
	defer func() { _ = repo.Close() }()
	w, err := repo.Worktree()
	if err != nil {
		panic(err)
	}
	commit, err := w.Commit("example commit", &CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@example.com",
			When:  time.UnixMicro(1234567890).UTC(),
		},
		Signer:            b64signer{},
		AllowEmptyCommits: true,
	})
	if err != nil {
		panic(err)
	}

	obj, err := repo.CommitObject(commit)
	if err != nil {
		panic(err)
	}
	fmt.Println(obj.Signature)
	// Output: dHJlZSA0YjgyNWRjNjQyY2I2ZWI5YTA2MGU1NGJmOGQ2OTI4OGZiZWU0OTA0CmF1dGhvciBKb2huIERvZSA8am9obkBleGFtcGxlLmNvbT4gMTIzNCArMDAwMApjb21taXR0ZXIgSm9obiBEb2UgPGpvaG5AZXhhbXBsZS5jb20+IDEyMzQgKzAwMDAKCmV4YW1wbGUgY29tbWl0
}

func isolateGitConfig(t *testing.T, gitconfig string) {
	t.Helper()

	home := t.TempDir()
	path := filepath.Join(home, ".gitconfig")
	require.NoError(t, os.WriteFile(path, []byte(gitconfig), 0o600))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", path)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_SYSTEM", "")

	resetPluginEntry("object-signer")
	t.Cleanup(func() { resetPluginEntry("object-signer") })
	resetPluginEntry("config-loader")
	t.Cleanup(registerTestConfigLoader)
	err := plugin.Register(plugin.ConfigLoader(), func() plugin.ConfigSource {
		return xconfig.NewAuto()
	})
	require.NoError(t, err)
}

func TestCommitGlobalGpgSignWithoutSigner(t *testing.T) { //nolint:paralleltest // modifies environment
	isolateGitConfig(t, "[commit]\n\tgpgSign = true\n")

	fs := memfs.New()
	r, err := Init(memory.NewStorage(), WithWorkTree(fs))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	require.NoError(t, err)
	require.NoError(t, util.WriteFile(fs, "file.txt", []byte("content"), 0o644))
	_, err = w.Add("file.txt")
	require.NoError(t, err)

	_, err = w.Commit("test commit\n", &CommitOptions{Author: defaultSignature()})
	require.ErrorIs(t, err, ErrNoObjectSigner)
}

func TestCreateTagGlobalGpgSignWithoutSigner(t *testing.T) { //nolint:paralleltest // modifies environment
	isolateGitConfig(t, "[tag]\n\tgpgSign = true\n")

	fs := memfs.New()
	r, err := Init(memory.NewStorage(), WithWorkTree(fs))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree()
	require.NoError(t, err)
	hash, err := w.Commit("initial\n", &CommitOptions{
		Author:            defaultSignature(),
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)

	_, err = r.CreateTag("v1", hash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "tag message\n",
	})
	require.ErrorIs(t, err, ErrNoObjectSigner)
}
