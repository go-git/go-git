package diff_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/diff"
)

func TestDefaultEdits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, new string
		want           []diff.Edit
	}{
		{name: "empty"},
		{name: "unchanged", old: "one\n", new: "one\n"},
		{name: "insert", new: "one\n", want: []diff.Edit{{After: diff.LineRange{End: 1}}}},
		{name: "delete", old: "one", want: []diff.Edit{{Before: diff.LineRange{End: 1}}}},
		{
			name: "replace", old: "same\nold\nend\n", new: "same\nnew\nend\n",
			want: []diff.Edit{{Before: diff.LineRange{Start: 1, End: 2}, After: diff.LineRange{Start: 1, End: 2}}},
		},
		{
			name: "shifted changes", old: "one\nold\ntwo\nthree\nlast",
			new: "one\na\nb\ntwo\nthree\nnew",
			want: []diff.Edit{
				{Before: diff.LineRange{Start: 1, End: 2}, After: diff.LineRange{Start: 1, End: 3}},
				{Before: diff.LineRange{Start: 4, End: 5}, After: diff.LineRange{Start: 5, End: 6}},
			},
		},
		{
			name: "newline", old: "one", new: "one\n",
			want: []diff.Edit{{Before: diff.LineRange{End: 1}, After: diff.LineRange{End: 1}}},
		},
		{
			name: "arbitrary bytes", old: "\xff\x00\r\n", new: "\xfe\x00\r\n",
			want: []diff.Edit{{Before: diff.LineRange{End: 1}, After: diff.LineRange{End: 1}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			oldContent, newContent := []byte(tc.old), []byte(tc.new)
			result, err := diff.NewDefault().Diff(t.Context(), oldContent, newContent)
			require.NoError(t, err)
			edits := result.Edits()
			require.Equal(t, tc.want, slices.Collect(edits))
			require.Equal(t, tc.want, slices.Collect(edits))
			require.Equal(t, tc.old, string(oldContent))
			require.Equal(t, tc.new, string(newContent))
		})
	}
}

func TestDefaultHunksMatchGit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, new string
		context, inter int
	}{
		{"empty", "", "", 3, 0},
		{"zero context replacement", "0\n1\n2", "0\n1", 0, 0},
		{"insert", "0\n2\n", "0\n1\n2\n", 0, 0},
		{"delete", "0\n1\n2\n", "0\n2\n", 0, 0},
		{"no final newline", "old", "new", 3, 0},
		{"CRLF", "0\r\nold\r\n", "0\r\nnew\r\n", 3, 0},
		{"invalid UTF-8", "\xff\n0\n", "\xfe\n0\n", 3, 0},
		{"empty lines", "0\n\n\n1\n", "0\n\n1\n", 1, 0},
		{"separate hunks", "0\nold\n2\n3\n4\n5\nold\n7\n", "0\nnew\n2\n3\n4\n5\nnew\n7\n", 1, 0},
		{"inter-hunk context", "0\nold\n2\n3\n4\n5\nold\n7\n", "0\nnew\n2\n3\n4\n5\nnew\n7\n", 1, 2},
		{"function name", "func example() {\n  0\n  old\n  2\n}\n", "func example() {\n  0\n  new\n  2\n}\n", 1, 0},
		{"huge context", "0\nold\n2\n", "0\nnew\n2\n", int(^uint(0) >> 1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := diff.NewDefault().Diff(t.Context(), []byte(tc.old), []byte(tc.new))
			require.NoError(t, err)
			options := diff.HunkOptions{
				Context: tc.context, InterHunkContext: tc.inter,
			}
			actual := render(t, result, options)
			require.Equal(t, diff.HunkOptions{
				Context: tc.context, InterHunkContext: tc.inter,
			}, options)

			// A context of 1000 already covers these complete fixtures.
			gitContext := min(tc.context, 1000)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "old"), []byte(tc.old), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "new"), []byte(tc.new), 0o644))
			cmd := gitenv.Command("git", "diff", "--no-index", "--no-ext-diff",
				"--no-textconv", "--no-color", "--text", "--diff-algorithm=myers",
				"--no-indent-heuristic", "-U"+strconv.Itoa(gitContext),
				"--inter-hunk-context="+strconv.Itoa(tc.inter), "old", "new")
			cmd.Dir = dir
			expected, err := cmd.CombinedOutput()
			if err != nil {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, "%s", expected)
				require.Equal(t, 1, exit.ExitCode(), "%s", expected)
			}
			if start := bytes.Index(expected, []byte("@@")); start >= 0 {
				expected = expected[start:]
			} else {
				expected = nil
			}
			require.Equal(t, string(expected), actual)
		})
	}
}

func TestDefaultCustomSection(t *testing.T) {
	t.Parallel()
	options := diff.DefaultOptions{
		FindSection: func(line []byte) (string, bool) {
			return "section", bytes.HasPrefix(line, []byte("#"))
		},
	}
	backend := diff.NewDefaultWithOptions(options)
	options.FindSection = nil
	result, err := backend.Diff(t.Context(),
		[]byte("# section\n  same\n  old\n"), []byte("# section\n  same\n  new\n"))
	require.NoError(t, err)
	require.Contains(t, render(t, result, diff.HunkOptions{}), "@@ -3 +3 @@ section\n")
}

func TestDefaultErrors(t *testing.T) {
	t.Parallel()
	backend := diff.NewDefault()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := backend.Diff(ctx, nil, nil)
	require.ErrorIs(t, err, context.Canceled)

	result, err := backend.Diff(t.Context(), []byte("old\n0\nold\n"), []byte("new\n0\nnew\n"))
	require.NoError(t, err)
	count := 0
	for hunk, err := range result.Hunks(ctx, diff.HunkOptions{}) {
		require.Zero(t, hunk)
		require.ErrorIs(t, err, context.Canceled)
		count++
	}
	require.Equal(t, 1, count)
	for _, options := range []diff.HunkOptions{{Context: -1}, {InterHunkContext: -1}} {
		count = 0
		for hunk, err := range result.Hunks(t.Context(), options) {
			require.Zero(t, hunk)
			require.ErrorContains(t, err, "negative context")
			count++
		}
		require.Equal(t, 1, count)
	}
}

func TestDefaultIteratorTermination(t *testing.T) {
	t.Parallel()
	sections := 0
	result, err := diff.NewDefaultWithOptions(diff.DefaultOptions{
		FindSection: func([]byte) (string, bool) {
			sections++
			return "section", true
		},
	}).Diff(t.Context(), []byte("old\nsame\nold\n"), []byte("new\nsame\nnew\n"))
	require.NoError(t, err)
	for range result.Edits() {
		break
	}
	require.Len(t, slices.Collect(result.Edits()), 2)
	hunks := result.Hunks(t.Context(), diff.HunkOptions{})
	for _, err := range hunks {
		require.NoError(t, err)
		break
	}
	require.Zero(t, sections, "a stopped iterator must not construct the next hunk")
	for range 2 {
		count := 0
		for _, err := range hunks {
			require.NoError(t, err)
			count++
		}
		require.Equal(t, 2, count)
	}
}

func TestDefaultHunkCancellation(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"hunk", "section"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			options := diff.DefaultOptions{}
			if stage == "section" {
				options.FindSection = func([]byte) (string, bool) { cancel(); return "same", true }
			}
			result, err := diff.NewDefaultWithOptions(options).Diff(ctx, []byte("same\nold\n"), []byte("same\nnew\n"))
			require.NoError(t, err)
			hunkCount, errors := 0, 0
			for hunk, err := range result.Hunks(ctx, diff.HunkOptions{}) {
				if err != nil {
					require.Zero(t, hunk)
					require.ErrorIs(t, err, context.Canceled)
					errors++
					continue
				}
				hunkCount++
				cancel()
			}
			require.Equal(t, 1, errors)
			if stage == "section" {
				require.Zero(t, hunkCount)
			} else {
				require.Equal(t, 1, hunkCount)
			}
		})
	}
}

func render(t *testing.T, result diff.Result, options diff.HunkOptions) string {
	t.Helper()
	var output bytes.Buffer
	for h, err := range result.Hunks(t.Context(), options) {
		require.NoError(t, err)
		writeRange := func(start, count int) string {
			if count != 0 {
				start++
			}
			if count == 1 {
				return strconv.Itoa(start)
			}
			return fmt.Sprintf("%d,%d", start, count)
		}
		fmt.Fprintf(&output, "@@ -%s +%s @@", writeRange(h.Before.Start, h.Before.End-h.Before.Start), writeRange(h.After.Start, h.After.End-h.After.Start))
		if h.Section != "" {
			fmt.Fprintf(&output, " %s", h.Section)
		}
		output.WriteByte('\n')
		for _, line := range h.Lines {
			prefix := map[diff.Operation]byte{diff.Equal: ' ', diff.Delete: '-', diff.Insert: '+'}
			output.WriteByte(prefix[line.Operation])
			output.Write(line.Content)
			if !bytes.HasSuffix(line.Content, []byte{'\n'}) {
				output.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return output.String()
}

func FuzzDefaultEdits(f *testing.F) {
	f.Add([]byte("one\nold\n"), []byte("one\nnew\n"))
	f.Add([]byte("\xff\x00"), []byte("\xff\x00\n"))
	f.Fuzz(func(t *testing.T, oldContent, newContent []byte) {
		result, err := diff.NewDefault().Diff(t.Context(), oldContent, newContent)
		require.NoError(t, err)
		records := func(data []byte) [][]byte {
			lines := bytes.SplitAfter(data, []byte{'\n'})
			if len(lines[len(lines)-1]) == 0 {
				lines = lines[:len(lines)-1]
			}
			return lines
		}
		oldLines, newLines := records(oldContent), records(newContent)
		var actual bytes.Buffer
		oldPos, newPos := 0, 0
		for edit := range result.Edits() {
			require.GreaterOrEqual(t, edit.Before.Start, oldPos)
			require.GreaterOrEqual(t, edit.After.Start, newPos)
			require.GreaterOrEqual(t, edit.Before.End, edit.Before.Start)
			require.GreaterOrEqual(t, edit.After.End, edit.After.Start)
			require.LessOrEqual(t, edit.Before.End, len(oldLines))
			require.LessOrEqual(t, edit.After.End, len(newLines))
			require.Equal(t, edit.Before.Start-oldPos, edit.After.Start-newPos)
			for _, line := range oldLines[oldPos:edit.Before.Start] {
				actual.Write(line)
			}
			for _, line := range newLines[edit.After.Start:edit.After.End] {
				actual.Write(line)
			}
			oldPos = edit.Before.End
			newPos = edit.After.End
		}
		require.Equal(t, len(oldLines)-oldPos, len(newLines)-newPos)
		for _, line := range oldLines[oldPos:] {
			actual.Write(line)
		}
		require.Equal(t, string(newContent), actual.String())
	})
}
