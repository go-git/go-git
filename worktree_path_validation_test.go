package git

import (
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// TestPathValidationRejectsDangerousResets verifies that go-git rejects
// resetting onto commits that contain dangerous paths. For each case, a
// commit is crafted with a tree containing a single bad path, then
// Reset(HardReset) is run against it; the diff machinery's per-change
// validation must reject before any worktree write happens.
//
// The upstream `git reset --hard` is not used as a comparator because
// its checkout path does not run verify_path on existing commits the
// way cherry-pick or merge do; v5 does not expose those operations on
// the Worktree. Conformance with upstream Git's path rules is verified
// indirectly via TestValidPath / TestWindowsValidPath.
func TestPathValidationRejectsDangerousResets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping path validation conformance test in short mode")
	}
	t.Parallel()

	tests := []struct {
		name string
		// path is the file path to place in the crafted commit's tree.
		// Nested paths (containing /) are built as nested tree objects.
		path string
		// config overrides to set on the repository before resetting.
		config map[string]string
		// onlyOnGOOS, when set, skips the case unless runtime.GOOS matches.
		onlyOnGOOS string
	}{
		{
			name: ".git at root",
			path: ".git/config",
		},
		{
			name: ".git in subdirectory",
			path: "subdir/.git/config",
		},
		{
			name: "git~1 8.3 short name",
			path: "git~1/config",
		},
		{
			name:   "NTFS trailing space on .git",
			path:   ".git /config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS trailing dot on .git",
			path:   ".git./config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS alternate data stream",
			path:   ".git::$INDEX_ALLOCATION/config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS reserved device name CON",
			path:   "CON/file",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS reserved device name NUL",
			path:   "NUL",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "HFS+ zero-width character in .git",
			path:   ".g\u200cit/config",
			config: map[string]string{"core.protectHFS": "true"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.onlyOnGOOS != "" && runtime.GOOS != tc.onlyOnGOOS {
				t.Skipf("only runs on %s", tc.onlyOnGOOS)
			}

			dir := t.TempDir()

			r, err := PlainInit(dir, false)
			require.NoError(t, err)

			w, err := r.Worktree()
			require.NoError(t, err)

			require.NoError(t, util.WriteFile(w.Filesystem, "README", []byte("init"), 0o644))
			_, err = w.Add("README")
			require.NoError(t, err)

			initHash, err := w.Commit("initial commit\n", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)

			cfg, err := r.Config()
			require.NoError(t, err)
			for k, v := range tc.config {
				switch k {
				case "core.protectNTFS":
					cfg.Core.ProtectNTFS = config.NewOptBool(v == "true")
				case "core.protectHFS":
					cfg.Core.ProtectHFS = config.NewOptBool(v == "true")
				default:
					t.Fatalf("unsupported config override: %s", k)
				}
			}
			require.NoError(t, r.Storer.SetConfig(cfg))

			initCommit, err := r.CommitObject(initHash)
			require.NoError(t, err)

			badCommit := buildBadCommit(t, r.Storer, initCommit, initHash, tc.path)

			err = w.Reset(&ResetOptions{Commit: badCommit.Hash, Mode: HardReset})
			assert.Error(t, err, "go-git should reject reset onto %q", tc.path)
		})
	}
}

func buildBadCommit(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, filePath string) *object.Commit {
	t.Helper()

	content := []byte("exploit")
	blobObj := s.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	blobObj.SetSize(int64(len(content)))
	bw, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = bw.Write(content)
	require.NoError(t, err)
	require.NoError(t, bw.Close())
	blobHash, err := s.SetEncodedObject(blobObj)
	require.NoError(t, err)

	parts := strings.Split(filePath, "/")
	leafHash := blobHash
	leafMode := filemode.Regular

	for i := len(parts) - 1; i >= 1; i-- {
		tree := &object.Tree{
			Entries: []object.TreeEntry{
				{Name: parts[i], Mode: leafMode, Hash: leafHash},
			},
		}
		treeObj := s.NewEncodedObject()
		require.NoError(t, tree.Encode(treeObj))
		leafHash, err = s.SetEncodedObject(treeObj)
		require.NoError(t, err)
		leafMode = filemode.Dir
	}

	parentTree, err := parent.Tree()
	require.NoError(t, err)

	entries := make([]object.TreeEntry, len(parentTree.Entries), len(parentTree.Entries)+1)
	copy(entries, parentTree.Entries)
	entries = append(entries, object.TreeEntry{
		Name: parts[0],
		Mode: leafMode,
		Hash: leafHash,
	})
	rootTree := &object.Tree{Entries: entries}
	sort.Sort(object.TreeEntrySorter(rootTree.Entries))
	rootObj := s.NewEncodedObject()
	require.NoError(t, rootTree.Encode(rootObj))
	rootHash, err := s.SetEncodedObject(rootObj)
	require.NoError(t, err)

	commit := &object.Commit{
		Author:       *defaultSignature(),
		Committer:    *defaultSignature(),
		Message:      "bad path: " + filePath + "\n",
		TreeHash:     rootHash,
		ParentHashes: []plumbing.Hash{parentHash},
	}
	commitObj := s.NewEncodedObject()
	require.NoError(t, commit.Encode(commitObj))
	commitHash, err := s.SetEncodedObject(commitObj)
	require.NoError(t, err)

	result, err := object.GetCommit(s, commitHash)
	require.NoError(t, err)
	return result
}
