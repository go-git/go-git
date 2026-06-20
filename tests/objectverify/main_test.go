//go:build linux

package objectverify_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/testutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// gpgEnv is the isolated GnuPG home created once per package run. The key
// is generated on demand at TestMain start and torn down at exit.
type gpgEnv struct {
	home   string
	keyID  string
	pubKey string
}

var gpg *gpgEnv

func TestMain(m *testing.M) {
	exitCode := runMain(m)
	testutil.TriggerLeakDetection()
	os.Exit(exitCode)
}

// This test verifies that current ways of signing objects didn't change in
// newer Git versions.
func TestMatchTestBehaviourWithGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping injection oracle")
	}
	if gpg == nil {
		t.Error("gpg not available")
		t.FailNow()
	}

	repo := initRepo(t)
	configureRepoSigning(t, repo, gpg.keyID)
	treeHash := gitWriteEmptyTree(t, repo)

	commitEnv := []string{
		"GIT_AUTHOR_NAME=Author",
		"GIT_AUTHOR_EMAIL=author@example.local",
		"GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=Author",
		"GIT_COMMITTER_EMAIL=author@example.local",
		"GIT_COMMITTER_DATE=1700000000 +0000",
	}

	t.Run("commit", func(t *testing.T) {
		t.Parallel()

		commitHash := gitCommitTreeSigned(t, repo, treeHash, "msg\n", commitEnv)
		gitRaw := readObjectBytes(t, repo, "commit", commitHash)

		c := decodeCommit(t, gitRaw)
		require.NotEmpty(t, c.Signature, "git commit-tree -S did not emit a gpgsig header")
		unsigned := encodeWithoutSignature(t, c)

		oursRaw := injectGpgSig(unsigned, c.Signature)

		require.Equal(t, gitRaw, oursRaw,
			"injectGpgSig output diverges from `git commit-tree -S`")
	})

	t.Run("tag", func(t *testing.T) {
		t.Parallel()

		commitHash := gitCommitTreeSigned(t, repo, treeHash, "msg for tag\n", commitEnv)
		tagHash := gitTagSigned(t, repo, "v0", commitHash, "tag msg\n", commitEnv)
		gitRaw := readObjectBytes(t, repo, "tag", tagHash)

		tag := decodeTag(t, gitRaw)
		require.NotEmpty(t, tag.Signature, "git tag -s did not emit an inline signature")
		unsigned := encodeTagWithoutSignature(t, tag)

		oursRaw := append(append([]byte{}, unsigned...), []byte(tag.Signature)...)

		require.Equal(t, gitRaw, oursRaw,
			"tag append output diverges from `git tag -s`")
	})
}

func runMain(m *testing.M) int {
	cleanup, ok := setupGPG()
	if cleanup != nil {
		defer cleanup()
	}
	if !ok {
		// Tests will skip individually when gpg is nil.
		return m.Run()
	}
	return m.Run()
}

func decodeCommit(t *testing.T, raw []byte) *object.Commit {
	t.Helper()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.CommitObject)
	_, err := obj.Write(raw)
	require.NoError(t, err)
	c := &object.Commit{}
	require.NoError(t, c.Decode(obj))
	return c
}

func decodeTag(t *testing.T, raw []byte) *object.Tag {
	t.Helper()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.TagObject)
	_, err := obj.Write(raw)
	require.NoError(t, err)
	tag := &object.Tag{}
	require.NoError(t, tag.Decode(obj))
	return tag
}

func encodeWithoutSignature(t *testing.T, c *object.Commit) []byte {
	t.Helper()
	out := &plumbing.MemoryObject{}
	require.NoError(t, c.EncodeWithoutSignature(out))
	return readMemoryObject(t, out)
}

func encodeTagWithoutSignature(t *testing.T, tag *object.Tag) []byte {
	t.Helper()
	out := &plumbing.MemoryObject{}
	require.NoError(t, tag.EncodeWithoutSignature(out))
	return readMemoryObject(t, out)
}

func readMemoryObject(t *testing.T, o *plumbing.MemoryObject) []byte {
	t.Helper()
	r, err := o.Reader()
	require.NoError(t, err)
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return b
}

func configureRepoSigning(t *testing.T, repo, keyID string) {
	t.Helper()
	// gpg.format is forced to openpgp because the host's global git
	// config may set it to ssh, which would reinterpret user.signingkey
	// as a path on disk.
	for _, kv := range [][2]string{
		{"user.signingkey", keyID},
		{"user.email", "author@example.local"},
		{"user.name", "Author"},
		{"gpg.format", "openpgp"},
		{"gpg.program", "gpg"},
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
	} {
		out, err := exec.Command("git", "-C", repo, "config", "--local", kv[0], kv[1]).CombinedOutput()
		require.NoErrorf(t, err, "git config %s: %s", kv[0], out)
	}
}

func gitWriteEmptyTree(t *testing.T, repo string) plumbing.Hash {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "write-tree")
	out, err := cmd.Output()
	require.NoError(t, err, "git write-tree")
	return plumbing.NewHash(strings.TrimSpace(string(out)))
}

func gitCommitTreeSigned(t *testing.T, repo string, tree plumbing.Hash, msg string, envOverrides []string) plumbing.Hash {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "commit-tree", "-S", "-m", strings.TrimRight(msg, "\n"), tree.String())
	cmd.Env = append(os.Environ(), envOverrides...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoErrorf(t, err, "git commit-tree -S: %s", stderr.String())
	return plumbing.NewHash(strings.TrimSpace(string(out)))
}

func gitTagSigned(t *testing.T, repo, name string, target plumbing.Hash, msg string, envOverrides []string) plumbing.Hash {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "tag", "-s", "-m", strings.TrimRight(msg, "\n"), name, target.String())
	cmd.Env = append(os.Environ(), envOverrides...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoErrorf(t, cmd.Run(), "git tag -s: %s", stderr.String())

	rev, err := exec.Command("git", "-C", repo, "rev-parse", name).Output()
	require.NoError(t, err)
	return plumbing.NewHash(strings.TrimSpace(string(rev)))
}

func readObjectBytes(t *testing.T, repo, kind string, h plumbing.Hash) []byte {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "cat-file", kind, h.String()).Output()
	require.NoError(t, err, "git cat-file %s %s", kind, h.String())
	return out
}

func setupGPG() (func(), bool) {
	if _, err := exec.LookPath("gpg"); err != nil {
		log.Printf("objectverify: skipping (gpg not in PATH): %v", err)
		return nil, false
	}
	if _, err := exec.LookPath("git"); err != nil {
		log.Printf("objectverify: skipping (git not in PATH): %v", err)
		return nil, false
	}

	home, err := os.MkdirTemp("", "gpg-objectverify-*")
	if err != nil {
		log.Printf("objectverify: tempdir: %v", err)
		return nil, false
	}
	if err := os.Chmod(home, 0o700); err != nil {
		log.Printf("objectverify: chmod home: %v", err)
		_ = os.RemoveAll(home)
		return nil, false
	}
	if err := os.Setenv("GNUPGHOME", home); err != nil {
		_ = os.RemoveAll(home)
		return nil, false
	}

	cleanup := func() {
		_ = exec.Command("gpgconf", "--kill", "all").Run()
		_ = os.RemoveAll(home)
		_ = os.Unsetenv("GNUPGHOME")
	}

	if err := generateGPGKey(); err != nil {
		log.Printf("objectverify: generate key: %v", err)
		return cleanup, false
	}

	keyID, err := readGPGKeyID()
	if err != nil {
		log.Printf("objectverify: read key id: %v", err)
		return cleanup, false
	}

	pub, err := exportArmoredPublicKey(keyID)
	if err != nil {
		log.Printf("objectverify: export key: %v", err)
		return cleanup, false
	}

	gpg = &gpgEnv{home: home, keyID: keyID, pubKey: pub}
	return cleanup, true
}

func generateGPGKey() error {
	cmd := exec.Command("gpg",
		"--batch", "--pinentry-mode", "loopback", "--passphrase", "",
		"--quick-generate-key",
		"go-git verify <verify@go-git.test>",
		"default", "default", "never")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gpg --quick-generate-key: %w: %s", err, out)
	}
	return nil
}

func readGPGKeyID() (string, error) {
	out, err := exec.Command("gpg", "--list-secret-keys", "--with-colons").Output()
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" {
			return fields[9], nil
		}
	}
	return "", errors.New("no fingerprint in gpg --list-secret-keys output")
}

func exportArmoredPublicKey(keyID string) (string, error) {
	out, err := exec.Command("gpg", "--armor", "--export", keyID).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// gpgSign produces an ASCII-armored detached signature over payload using
// the test key.
func gpgSign(t *testing.T, payload []byte) string {
	t.Helper()
	cmd := exec.Command("gpg",
		"--batch", "--pinentry-mode", "loopback", "--passphrase", "",
		"--armor", "--detach-sign", "--local-user", gpg.keyID)
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoErrorf(t, err, "gpg --detach-sign: %s", stderr.String())
	return string(out)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput()
	require.NoErrorf(t, err, "git init: %s", out)
	return dir
}

func writeLooseObject(t *testing.T, repo, objType string, content []byte) plumbing.Hash {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "hash-object", "-w", "--literally", "-t", objType, "--stdin")
	cmd.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoErrorf(t, err, "git hash-object: %s", stderr.String())
	return plumbing.NewHash(strings.TrimSpace(string(out)))
}

func gitVerifyCommit(t *testing.T, repo string, h plumbing.Hash) error {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "verify-commit", h.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func gitVerifyTag(t *testing.T, repo string, h plumbing.Hash) error {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "verify-tag", h.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func injectGpgSig(unsigned []byte, sig string) []byte {
	sep := []byte("\n\n")
	idx := bytes.Index(unsigned, sep)
	if idx < 0 {
		panic("injectGpgSig: no header/body separator")
	}
	sig = strings.TrimSuffix(sig, "\n")
	indented := strings.ReplaceAll(sig, "\n", "\n ")
	headerLine := "gpgsig " + indented + "\n"

	var buf bytes.Buffer
	buf.Write(unsigned[:idx+1]) // up to and including the \n that ends the last header
	buf.WriteString(headerLine) // gpgsig + indented sig + closing \n
	buf.Write(unsigned[idx+1:]) // empty-line \n + body
	return buf.Bytes()
}
