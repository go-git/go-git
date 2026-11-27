package object

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/diff"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	fdiff "github.com/go-git/go-git/v6/plumbing/format/diff"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestPatchOptions(t *testing.T) {
	t.Parallel()
	for _, entryPoint := range []string{"change", "changes", "tree", "commit"} {
		t.Run(entryPoint, func(t *testing.T) {
			t.Parallel()
			storage := memory.NewStorage()
			trees := make([]*Tree, 2)
			commits := make([]*Commit, 2)
			for i, hash := range []string{
				"1111111111111111111111111111111111111111",
				"2222222222222222222222222222222222222222",
			} {
				tree := &Tree{s: storage, Entries: []TreeEntry{{
					Name: "module", Mode: filemode.Submodule, Hash: plumbing.NewHash(hash),
				}}}
				o := storage.NewEncodedObject()
				require.NoError(t, tree.Encode(o))
				treeHash, err := storage.SetEncodedObject(o)
				require.NoError(t, err)
				tree.Hash = treeHash
				trees[i] = tree
				commits[i] = &Commit{s: storage, TreeHash: treeHash}
			}
			change := &Change{
				From: ChangeEntry{Name: "module", Tree: trees[0], TreeEntry: trees[0].Entries[0]},
				To:   ChangeEntry{Name: "module", Tree: trees[1], TreeEntry: trees[1].Entries[0]},
			}
			backend := &patchTestDiffer{}
			options := &PatchOptions{Differ: backend}
			run := func() (*Patch, error) {
				switch entryPoint {
				case "change":
					return change.PatchWithOptions(t.Context(), options)
				case "changes":
					return Changes{change}.PatchWithOptions(t.Context(), options)
				case "tree":
					return trees[0].PatchWithOptions(t.Context(), trees[1], options)
				default:
					return commits[0].PatchWithOptions(t.Context(), commits[1], options)
				}
			}
			patch, err := run()
			require.NoError(t, err)
			require.Equal(t, 1, backend.calls)
			require.Same(t, backend, options.Differ)
			require.Contains(t, patch.String(), "-Subproject commit 1111111111111111111111111111111111111111")
			require.Contains(t, patch.String(), "+Subproject commit 2222222222222222222222222222222222222222")
			stats := patch.Stats()
			require.Equal(t, FileStats{{Name: "module", Addition: 1, Deletion: 1}}, stats)

			backend.err = errors.New("backend failed")
			_, err = run()
			require.ErrorIs(t, err, backend.err)
			backend.err = nil
			backend.nilResult = true
			_, err = run()
			require.ErrorContains(t, err, "nil result")
		})
	}
}

type patchTestDiffer struct {
	calls     int
	err       error
	nilResult bool
}

func (d *patchTestDiffer) Diff(ctx context.Context, oldContent, newContent []byte) (diff.Result, error) {
	d.calls++
	if d.err != nil || d.nilResult {
		return nil, d.err
	}
	return diff.NewDefault().Diff(ctx, oldContent, newContent)
}

func TestPatchStatsIgnoreDisplayFilters(t *testing.T) {
	t.Parallel()
	result, err := diff.NewDefault().Diff(t.Context(), []byte("old\n"), []byte("new\nextra\n"))
	require.NoError(t, err)
	patch := &Patch{filePatches: []fdiff.FilePatch{&textFilePatch{
		result: statsOnlyResult{result},
		from: ChangeEntry{
			Name: "file", TreeEntry: TreeEntry{Mode: filemode.Regular},
		},
		to: ChangeEntry{
			Name: "file", TreeEntry: TreeEntry{Mode: filemode.Regular},
		},
	}}}
	require.Empty(t, patch.String())
	require.Equal(t, FileStats{{Name: "file", Addition: 2, Deletion: 1}}, patch.Stats())
}

func TestPatchOptionsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Changes(nil).PatchWithOptions(ctx, nil)
	require.ErrorIs(t, err, ErrCanceled)
}

type statsOnlyResult struct{ diff.Result }

func (statsOnlyResult) Hunks(context.Context, diff.HunkOptions) iter.Seq2[diff.Hunk, error] {
	return func(func(diff.Hunk, error) bool) {}
}
