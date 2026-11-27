package diff

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"

	textdiff "github.com/go-git/go-git/v6/plumbing/diff"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

func TestUnifiedEncoderOptions(t *testing.T) {
	t.Parallel()
	file := testFilePatch{
		from: &testFile{path: "file", mode: filemode.Regular, seed: "same\nold\n"},
		to:   &testFile{path: "file", mode: filemode.Regular, seed: "same\nnew\n"},
	}
	result := &encoderTestResult{Result: file.Diff()}
	file.result = result
	options := textdiff.HunkOptions{Context: 1, InterHunkContext: 4}
	var output bytes.Buffer
	encoder := NewUnifiedEncoder(&output, 0).SetHunkOptions(options)
	require.NoError(t, encoder.EncodeContext(t.Context(), testPatch{filePatches: []testFilePatch{file}}))
	require.Equal(t, options, result.options)
	require.Equal(t, 1, options.Context)
	require.Contains(t, output.String(), " same\n-old\n+new\n")

	result.err = errors.New("hunk construction failed")
	output.Reset()
	require.ErrorIs(t, encoder.Encode(testPatch{filePatches: []testFilePatch{file}}), result.err)
	require.Empty(t, output.String())
	result.err = nil
	output.Reset()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, encoder.EncodeContext(ctx, testPatch{filePatches: []testFilePatch{file}}), context.Canceled)
	require.Empty(t, output.String())

	require.ErrorContains(t, encoder.SetHunkOptions(textdiff.HunkOptions{Context: -1}).
		Encode(testPatch{filePatches: []testFilePatch{file}}), "negative context")
}

func TestUnifiedEncoderSuppressedChanges(t *testing.T) {
	t.Parallel()
	for _, mode := range []filemode.FileMode{filemode.Regular, filemode.Executable} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			file := testFilePatch{
				from: &testFile{path: "file", mode: filemode.Regular, seed: "# old\n"},
				to:   &testFile{path: "file", mode: mode, seed: "# new\n"},
			}
			file.result = &encoderTestResult{Result: file.Diff(), suppress: true}
			var output bytes.Buffer
			require.NoError(t, NewUnifiedEncoder(&output, 3).
				Encode(testPatch{filePatches: []testFilePatch{file}}))
			if mode == filemode.Regular {
				require.Empty(t, output.String())
			} else {
				require.Contains(t, output.String(), "old mode 100644\nnew mode 100755\n")
				require.NotContains(t, output.String(), "--- ")
				require.NotContains(t, output.String(), "@@")
			}
		})
	}
}

type encoderTestResult struct {
	textdiff.Result
	options  textdiff.HunkOptions
	err      error
	suppress bool
}

func (r *encoderTestResult) Edits() iter.Seq[textdiff.Edit] {
	panic("encoder must not traverse raw edits")
}

func (r *encoderTestResult) Hunks(ctx context.Context, options textdiff.HunkOptions) iter.Seq2[textdiff.Hunk, error] {
	r.options = options
	return func(yield func(textdiff.Hunk, error) bool) {
		if r.err != nil {
			yield(textdiff.Hunk{}, r.err)
			return
		}
		if !r.suppress {
			r.Result.Hunks(ctx, options)(yield)
		}
	}
}
