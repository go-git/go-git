package diff

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

func FuzzUnifiedEncoder(f *testing.F) {
	f.Add([]byte{}, []byte{}, uint8(0))
	f.Add([]byte("old"), []byte("new"), uint8(0))
	f.Add([]byte("a\r\nb\r\n"), []byte("a\r\nc\r\n"), uint8(3))
	f.Add([]byte{0xff, '\n', 0}, []byte{0xfe, '\n', 0}, uint8(1))
	f.Add([]byte("A\nY\nZ"), []byte("A\nY"), uint8(0))
	f.Fuzz(func(t *testing.T, oldContent, newContent []byte, contextLines uint8) {
		if len(oldContent)+len(newContent) > 16*1024 {
			t.Skip()
		}
		contextLines %= 8
		patch := testFilePatch{
			from: &testFile{path: "file", mode: filemode.Regular, seed: string(oldContent)},
			to:   &testFile{path: "file", mode: filemode.Regular, seed: string(newContent)},
		}
		result := patch.Diff()
		patch.result = result
		var actual bytes.Buffer
		require.NoError(t, NewUnifiedEncoder(&actual, int(contextLines)).
			Encode(testPatch{filePatches: []testFilePatch{patch}}))
		if actual.Len() == 0 {
			require.Equal(t, oldContent, newContent)
			return
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(file, oldContent, 0o644))
		cmd := gitenv.Command("git", "apply", "--unidiff-zero", "--whitespace=nowarn", "-")
		cmd.Dir = dir
		cmd.Stdin = &actual
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, string(newContent), string(content))
	})
}
