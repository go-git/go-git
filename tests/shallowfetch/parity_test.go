package shallowfetch_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// shallowSet reads a repository's .git/shallow into a set of hex hashes.
// An absent file is an empty set.
func shallowSet(t *testing.T, repo string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".git", "shallow"))
	if os.IsNotExist(err) {
		return map[string]bool{}
	}
	require.NoError(t, err)
	set := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			set[line] = true
		}
	}
	return set
}

// refsOf returns the ref/SHA pairs of a repository, sorted.
func refsOf(t *testing.T, repo string) []string {
	t.Helper()
	out := gitOut(t, repo, "show-ref")
	var refs []string
	if out == "" {
		return refs
	}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		require.Len(t, fields, 2, "unexpected show-ref line: %q", line)
		refs = append(refs, fields[1]+" "+fields[0])
	}
	sort.Strings(refs)
	return refs
}

// objectsOf returns the sorted set of objects reachable from all refs.
func objectsOf(t *testing.T, repo string) []string {
	t.Helper()
	out := gitOut(t, repo, "rev-list", "--objects", "--all")
	var objects []string
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		objects = append(objects, strings.TrimSpace(line))
	}
	sort.Strings(objects)
	return objects
}

// worktreeFiles returns relpath -> content for every file below repo,
// excluding .git.
func worktreeFiles(t *testing.T, repo string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(repo, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	require.NoError(t, err)
	return files
}

// assertRepoParity asserts that the go-git-produced repository ours matches
// the git-CLI-produced twin on every observable a shallow fetch can move:
// the shallow boundary, the refs, the reachable object set, the worktree,
// and internal consistency (fsck).
func assertRepoParity(t *testing.T, ours, twin string) {
	t.Helper()

	git(t, ours, "fsck", "--no-dangling")

	require.Equal(t, shallowSet(t, twin), shallowSet(t, ours),
		".git/shallow must match the git CLI twin")
	require.ElementsMatch(t, refsOf(t, twin), refsOf(t, ours),
		"refs must match the git CLI twin")
	require.ElementsMatch(t, objectsOf(t, twin), objectsOf(t, ours),
		"reachable objects must match the git CLI twin")
	require.Equal(t, worktreeFiles(t, twin), worktreeFiles(t, ours),
		"worktree must match the git CLI twin")

	status := gitOut(t, ours, "status", "--porcelain")
	require.Empty(t, status, "go-git worktree must be clean after reset")
}
