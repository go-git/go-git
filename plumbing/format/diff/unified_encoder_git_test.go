package diff

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/color"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

func TestUnifiedEncoderMatchesGit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, new string
		context        int
	}{
		{"zero context replacement", "A\nY\nZ", "A\nY", 0},
		{"insert", "A\nB\n", "A\nnew\nB\n", 0},
		{"delete", "A\nold\nB\n", "A\nB\n", 0},
		{"no final newline", "old", "new", 3},
		{"CRLF", "same\r\nold\r\n", "same\r\nnew\r\n", 3},
		{"invalid UTF-8", "\xff\nsame\n", "\xfe\nsame\n", 3},
		{"repeated lines", "a\nb\na\nb\nc\n", "a\nb\nc\na\nb\n", 1},
		{"indent heuristic", "f() {\n  one();\n}\n", "f() {\n  one();\n}\n\ng() {\n  two();\n}\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "old"), []byte(tc.old), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "new"), []byte(tc.new), 0o644))
			expected := gitDiff(t, dir, "--no-color", "-U"+strconv.Itoa(tc.context), "old", "new")
			patch := testPatch{filePatches: []testFilePatch{{
				from: &testFile{path: "file", mode: filemode.Regular, seed: tc.old},
				to:   &testFile{path: "file", mode: filemode.Regular, seed: tc.new},
			}}}
			var actual bytes.Buffer
			require.NoError(t, NewUnifiedEncoder(&actual, tc.context).Encode(patch))
			require.Equal(t, string(hunkBody(expected)), string(hunkBody(actual.Bytes())))
		})
	}
}

func TestUnifiedEncoderEmptyFileMatchesGit(t *testing.T) {
	t.Parallel()
	for _, deletion := range []bool{false, true} {
		t.Run(strconv.FormatBool(deletion), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), nil, 0o644))
			patch := testFilePatch{to: &testFile{path: "file", mode: filemode.Regular}}
			oldPath, newPath := "/dev/null", "file"
			if deletion {
				patch.from, patch.to = patch.to, nil
				oldPath, newPath = newPath, oldPath
			}
			expected := gitDiff(t, dir, "--no-color", oldPath, newPath)
			var actual bytes.Buffer
			require.NoError(t, NewUnifiedEncoder(&actual, DefaultContextLines).
				Encode(testPatch{filePatches: []testFilePatch{patch}}))
			require.Equal(t, string(expected), actual.String())
		})
	}
}

func TestUnifiedEncoderColorLineEndings(t *testing.T) {
	t.Parallel()
	for _, ending := range []string{"\r\n", ""} {
		t.Run(strconv.Quote(ending), func(t *testing.T) {
			t.Parallel()
			patch := testPatch{filePatches: []testFilePatch{{
				from: &testFile{path: "file", seed: "old" + ending},
				to:   &testFile{path: "file", seed: "new" + ending},
			}}}
			var actual bytes.Buffer
			require.NoError(t, NewUnifiedEncoder(&actual, 0).SetColor(NewColorConfig()).Encode(patch))
			require.Contains(t, actual.String(), color.Red+"-old"+color.Reset+ending)
			require.Contains(t, actual.String(), color.Green+"+new"+color.Reset+ending)
			if ending == "" {
				require.Contains(t, actual.String(), color.Reset+"\n\\ No newline at end of file\n")
			}
		})
	}
}

func TestUnifiedEncoderWriterErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("write failed")
	for _, tc := range []struct {
		name string
		w    io.Writer
		err  error
	}{
		{"failure", encoderTestWriter{want}, want},
		{"short write", encoderTestWriter{}, io.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, NewUnifiedEncoder(tc.w, 0).Encode(oneChunkPatch), tc.err)
		})
	}
}

type encoderTestWriter struct{ err error }

func (w encoderTestWriter) Write([]byte) (int, error) { return 0, w.err }

func gitDiff(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := gitenv.Command("git", append([]string{
		"diff", "--no-index", "--no-ext-diff", "--no-textconv", "--text",
		"--full-index", "--diff-algorithm=myers", "--no-indent-heuristic",
	}, args...)...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("git diff: %v\n%s", err, output)
		}
	}
	return output
}

func hunkBody(patch []byte) []byte {
	if start := bytes.Index(patch, []byte("@@")); start >= 0 {
		return patch[start:]
	}
	return nil
}
