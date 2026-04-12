package compat_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

func TestTranslateObjectMatchesUpstreamGit(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()
	sha1Dir := filepath.Join(root, "repo-sha1")
	sha256Dir := filepath.Join(root, "repo-sha256")

	initGitRepo(t, sha1Dir, "sha1")
	initGitRepo(t, sha256Dir, "sha256")

	sha1Blob, sha1Tree, sha1Commit, sha1Tag := createObjectChain(t, sha1Dir)
	sha256Blob, sha256Tree, sha256Commit, sha256Tag := createObjectChain(t, sha256Dir)

	repo, err := git.PlainOpen(sha1Dir)
	require.NoError(t, err)

	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, compat.NewMemoryMapping())
	require.NoError(t, compat.TranslateStoredObjects(repo.Storer, tr))

	tests := []struct {
		name     string
		objType  plumbing.ObjectType
		native   plumbing.Hash
		expected string
	}{
		{name: "blob", objType: plumbing.BlobObject, native: plumbing.NewHash(sha1Blob), expected: sha256Blob},
		{name: "tree", objType: plumbing.TreeObject, native: plumbing.NewHash(sha1Tree), expected: sha256Tree},
		{name: "commit", objType: plumbing.CommitObject, native: plumbing.NewHash(sha1Commit), expected: sha256Commit},
		{name: "tag", objType: plumbing.TagObject, native: plumbing.NewHash(sha1Tag), expected: sha256Tag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			obj, err := repo.Storer.EncodedObject(tt.objType, tt.native)
			require.NoError(t, err)

			got, err := tr.TranslateObject(obj)
			require.NoError(t, err)

			assert.Equal(t, tt.expected, got.String())

			mapped, err := tr.Mapping().NativeToCompat(tt.native)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, mapped.String())
		})
	}
}

func TestTranslateContentMatchesUpstreamGit(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()
	sha1Dir := filepath.Join(root, "repo-sha1")
	sha256Dir := filepath.Join(root, "repo-sha256")

	initGitRepo(t, sha1Dir, "sha1")
	initGitRepo(t, sha256Dir, "sha256")

	sha1Blob, sha1Tree, sha1Commit, sha1Tag := createObjectChain(t, sha1Dir)
	sha256Blob, sha256Tree, sha256Commit, sha256Tag := createObjectChain(t, sha256Dir)

	repo, err := git.PlainOpen(sha1Dir)
	require.NoError(t, err)

	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, compat.NewMemoryMapping())
	require.NoError(t, compat.TranslateStoredObjects(repo.Storer, tr))

	tests := []struct {
		name     string
		objType  plumbing.ObjectType
		native   string
		expected string
	}{
		{name: "blob", objType: plumbing.BlobObject, native: sha1Blob, expected: sha256Blob},
		{name: "tree", objType: plumbing.TreeObject, native: sha1Tree, expected: sha256Tree},
		{name: "commit", objType: plumbing.CommitObject, native: sha1Commit, expected: sha256Commit},
		{name: "tag", objType: plumbing.TagObject, native: sha1Tag, expected: sha256Tag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			obj, err := repo.Storer.EncodedObject(tt.objType, plumbing.NewHash(tt.native))
			require.NoError(t, err)

			nativeRaw := readObjectContent(t, obj)
			_, err = tr.TranslateObject(obj)
			require.NoError(t, err)

			gotRaw, err := tr.ReverseTranslateContent(tt.objType, nativeRaw)
			require.NoError(t, err)

			expectedRaw := []byte(mustRunCmd(t, sha256Dir, nil, "git", "cat-file", tt.objType.String(), tt.expected))
			assert.Equal(t, expectedRaw, gotRaw)

			gotHash, err := tr.ComputeCompatHash(tt.objType, gotRaw)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, gotHash.String())

			gotType := strings.TrimSpace(mustRunCmd(t, sha256Dir, nil, "git", "cat-file", "-t", tt.expected))
			assert.Equal(t, tt.objType.String(), gotType)

			gotSize := strings.TrimSpace(mustRunCmd(t, sha256Dir, nil, "git", "cat-file", "-s", tt.expected))
			assert.Equal(t, strconv.Itoa(len(expectedRaw)), gotSize)
			assert.Equal(t, strconv.Itoa(len(gotRaw)), gotSize)
		})
	}
}

func TestTranslateSignedObjectsMatchesUpstreamGit(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()
	sha1Dir := filepath.Join(root, "repo-sha1")
	sha256Dir := filepath.Join(root, "repo-sha256")

	initGitRepo(t, sha1Dir, "sha1")
	initGitRepo(t, sha256Dir, "sha256")

	sha1Blob, sha1Tree, sha1Commit, _ := createObjectChain(t, sha1Dir)
	_, sha256Tree, sha256Commit, _ := createObjectChain(t, sha256Dir)

	sha1SignedCommit, sha1SignedTag := createSignedObjects(t, sha1Dir, sha1Tree, sha1Commit)
	sha256SignedCommit, sha256SignedTag := createSignedObjects(t, sha256Dir, sha256Tree, sha256Commit)

	repo, err := git.PlainOpen(sha1Dir)
	require.NoError(t, err)

	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA1,
		Compat: format.SHA256,
	}, compat.NewMemoryMapping())

	for _, dep := range []struct {
		objType plumbing.ObjectType
		hash    string
	}{
		{objType: plumbing.BlobObject, hash: sha1Blob},
		{objType: plumbing.TreeObject, hash: sha1Tree},
		{objType: plumbing.CommitObject, hash: sha1Commit},
	} {
		obj, err := repo.Storer.EncodedObject(dep.objType, plumbing.NewHash(dep.hash))
		require.NoError(t, err)
		_, err = tr.TranslateObject(obj)
		require.NoError(t, err)
	}

	tests := []struct {
		name     string
		objType  plumbing.ObjectType
		native   string
		expected string
	}{
		{name: "signed-commit", objType: plumbing.CommitObject, native: sha1SignedCommit, expected: sha256SignedCommit},
		{name: "signed-tag", objType: plumbing.TagObject, native: sha1SignedTag, expected: sha256SignedTag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			obj, err := repo.Storer.EncodedObject(tt.objType, plumbing.NewHash(tt.native))
			require.NoError(t, err)

			gotHash, err := tr.TranslateObject(obj)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, gotHash.String())

			nativeRaw := readObjectContent(t, obj)
			gotRaw, err := tr.ReverseTranslateContent(tt.objType, nativeRaw)
			require.NoError(t, err)

			expectedRaw := []byte(mustRunCmd(t, sha256Dir, nil, "git", "cat-file", tt.objType.String(), tt.expected))
			assert.Equal(t, expectedRaw, gotRaw)
		})
	}
}

func TestTranslateStoredObjectsMatchesUpstreamGitReverse(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()
	sha1Dir := filepath.Join(root, "repo-sha1")
	sha256Dir := filepath.Join(root, "repo-sha256")

	initGitRepo(t, sha1Dir, "sha1")
	initGitRepo(t, sha256Dir, "sha256")

	sha1Blob, sha1Tree, sha1Commit, sha1Tag := createObjectChain(t, sha1Dir)
	sha256Blob, sha256Tree, sha256Commit, sha256Tag := createObjectChain(t, sha256Dir)

	repo, err := git.PlainOpen(sha256Dir)
	require.NoError(t, err)

	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, compat.NewMemoryMapping())
	require.NoError(t, compat.TranslateStoredObjects(repo.Storer, tr))

	tests := []struct {
		name     string
		native   string
		expected string
	}{
		{name: "blob", native: sha256Blob, expected: sha1Blob},
		{name: "tree", native: sha256Tree, expected: sha1Tree},
		{name: "commit", native: sha256Commit, expected: sha1Commit},
		{name: "tag", native: sha256Tag, expected: sha1Tag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mapped, err := tr.Mapping().NativeToCompat(plumbing.NewHash(tt.native))
			require.NoError(t, err)
			assert.Equal(t, tt.expected, mapped.String())
		})
	}
}

func initGitRepo(t *testing.T, dir, objectFormat string) {
	t.Helper()

	output, err := runCmd("", nil, "git", "init", "--object-format="+objectFormat, dir)
	if err != nil {
		if objectFormat == "sha256" && strings.Contains(output, "unknown option") {
			t.Skip("installed git does not support --object-format")
		}
		if objectFormat == "sha256" && strings.Contains(strings.ToLower(output), "sha256") {
			t.Skipf("installed git does not support sha256 repositories: %s", output)
		}
		require.NoError(t, err, output)
	}
}

func createObjectChain(t *testing.T, dir string) (blob, tree, commit, tag string) {
	t.Helper()

	env := []string{
		"GIT_AUTHOR_NAME=Compat Test",
		"GIT_AUTHOR_EMAIL=compat@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=Compat Test",
		"GIT_COMMITTER_EMAIL=compat@example.com",
		"GIT_COMMITTER_DATE=1700000000 +0000",
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello compat\n"), 0o644))

	blob = strings.TrimSpace(mustRunCmd(t, dir, env, "git", "hash-object", "-w", "hello.txt"))
	mustRunCmd(t, dir, env, "git", "update-index", "--add", "--cacheinfo", "100644", blob, "hello.txt")
	tree = strings.TrimSpace(mustRunCmd(t, dir, env, "git", "write-tree"))
	commit = strings.TrimSpace(mustRunCmd(t, dir, env, "git", "commit-tree", tree, "-m", "Initial commit"))
	mustRunCmd(t, dir, env, "git", "update-ref", "refs/heads/main", commit)
	mustRunCmd(t, dir, env, "git", "tag", "-a", "-m", "v1.0", "v1.0", commit)
	tag = strings.TrimSpace(mustRunCmd(t, dir, env, "git", "rev-parse", "refs/tags/v1.0"))

	return blob, tree, commit, tag
}

func createSignedObjects(t *testing.T, dir, treeHash, commitHash string) (signedCommit, signedTag string) {
	t.Helper()

	commitContent := "" +
		"tree " + treeHash + "\n" +
		"author Compat Test <compat@example.com> 1700000000 +0000\n" +
		"committer Compat Test <compat@example.com> 1700000000 +0000\n" +
		multilineHeader("gpgsig", fakeSignature("primary-signature")) +
		multilineHeader("gpgsig-sha256", fakeSignature("compat-signature")) +
		"\n" +
		"Signed commit\n"
	signedCommit = strings.TrimSpace(mustRunCmdInput(t, dir, nil, commitContent, "git", "hash-object", "-t", "commit", "-w", "--stdin"))

	tagContent := "" +
		"object " + commitHash + "\n" +
		"type commit\n" +
		"tag signed-v1.0\n" +
		"tagger Compat Test <compat@example.com> 1700000000 +0000\n" +
		"\n" +
		"Signed tag\n" +
		fakeSignature("tag-signature")
	signedTag = strings.TrimSpace(mustRunCmdInput(t, dir, nil, tagContent, "git", "hash-object", "-t", "tag", "-w", "--stdin"))

	return signedCommit, signedTag
}

func multilineHeader(name, value string) string {
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(lines[0])
	b.WriteByte('\n')
	for _, line := range lines[1:] {
		b.WriteByte(' ')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func fakeSignature(label string) string {
	return "" +
		"-----BEGIN PGP SIGNATURE-----\n" +
		"\n" +
		label + "\n" +
		"-----END PGP SIGNATURE-----\n"
}

func mustRunCmd(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()

	output, err := runCmd(dir, env, name, args...)
	require.NoError(t, err, output)
	return output
}

func mustRunCmdInput(
	t *testing.T,
	dir string,
	env []string,
	input string,
	name string,
	args ...string,
) string {
	t.Helper()

	output, err := runCmdInput(dir, env, input, name, args...)
	require.NoError(t, err, output)
	return output
}

func readObjectContent(t *testing.T, obj plumbing.EncodedObject) []byte {
	t.Helper()

	reader, err := obj.Reader()
	require.NoError(t, err)
	defer reader.Close()

	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	return content
}

func runCmd(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}

func runCmdInput(
	dir string,
	env []string,
	input string,
	name string,
	args ...string,
) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}
