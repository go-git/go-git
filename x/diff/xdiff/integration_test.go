package xdiff_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	native "github.com/go-git/go-xdiff"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/diff"
	fdiff "github.com/go-git/go-git/v6/plumbing/format/diff"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/go-git/go-git/v6/x/diff/xdiff"
)

func TestPatchAndBlameIntegration(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp(".", ".git-reference-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	revisions := make([]*object.Commit, 0, 2)
	for i, content := range []struct{ functions, note, blank string }{
		{"f() {\n one\n}\n", "before", "kept\n"},
		{"f() {\n one\n}\n\ng() {\n two\n}\n", "after", "kept\n\n"},
	} {
		for name, text := range map[string]string{
			"functions.c": content.functions, "note.txt": content.note, "blank.txt": content.blank,
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := worktree.Add(name); err != nil {
				t.Fatal(err)
			}
		}
		hash, err := worktree.Commit("fixture", &git.CommitOptions{
			Author: &object.Signature{
				Name: "Test Fixture", Email: "fixture@example.invalid",
				When: time.Unix(int64(i+1), 0).UTC(),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		commit, err := repo.CommitObject(hash)
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, commit)
	}

	ctx := context.Background()
	{
		patch, err := revisions[0].PatchWithOptions(ctx, revisions[1],
			&object.PatchOptions{Differ: xdiff.New(&xdiff.Options{FunctionContext: true})})
		if err != nil {
			t.Fatal(err)
		}
		stats := patch.Stats()
		want := object.FileStats{
			{Name: "blank.txt", Addition: 1},
			{Name: "functions.c", Addition: 4},
			{Name: "note.txt", Addition: 1, Deletion: 1},
		}
		if !reflect.DeepEqual(stats, want) {
			t.Fatalf("stats=%+v, want %+v", stats, want)
		}
		for _, file := range patch.FilePatches() {
			if file.Diff() == nil {
				t.Fatal("text patch lost its neutral result")
			}
		}
		var output bytes.Buffer
		err = fdiff.NewUnifiedEncoder(&output, 0).
			SetHunkOptions(diff.HunkOptions{}).
			EncodeContext(ctx, patch)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "+g() {\n+ two\n+}\n") ||
			strings.Count(output.String(), "\\ No newline at end of file\n") != 2 {
			t.Fatalf("unexpected unified output: %s", output.String())
		}
		filtered, err := revisions[0].PatchWithOptions(ctx, revisions[1],
			&object.PatchOptions{Differ: xdiff.New(&xdiff.Options{Diff: native.Options{
				IgnoreBlankLines: true, IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("(?s).*")},
			}})})
		if err != nil {
			t.Fatal(err)
		}
		filteredStats := filtered.Stats()
		if !reflect.DeepEqual(filteredStats, want) {
			t.Fatalf("output filters changed raw stats: %+v", filteredStats)
		}
		output.Reset()
		if err := fdiff.NewUnifiedEncoder(&output, 0).EncodeContext(ctx, filtered); err != nil {
			t.Fatal(err)
		}
		if output.Len() != 0 {
			t.Fatalf("output filters leaked content or headers: %q", output.String())
		}
		blankFiltered, err := revisions[0].PatchWithOptions(ctx, revisions[1],
			&object.PatchOptions{Differ: xdiff.New(&xdiff.Options{Diff: native.Options{IgnoreBlankLines: true}})})
		if err != nil {
			t.Fatal(err)
		}
		if err := fdiff.NewUnifiedEncoder(&output, 0).EncodeContext(ctx, blankFiltered); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "blank.txt") ||
			!strings.Contains(output.String(), "functions.c") ||
			!strings.Contains(output.String(), "note.txt") {
			t.Fatalf("blank-line filter leaked headers or hid other files: %q", output.String())
		}
	}

	{
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("Git is not available")
		}
		// Git keeps the first closing brace in its original commit.
		cmd := exec.Command("git", "-c", "blame.ignoreRevsFile=", "-C", dir,
			"blame", "--line-porcelain", revisions[1].Hash.String(), "--", "functions.c")
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git blame: %v\n%s", err, output)
		}
		var want []plumbing.Hash
		for line := range strings.SplitSeq(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && len(fields[0]) == 40 {
				want = append(want, plumbing.NewHash(fields[0]))
			}
		}
		if len(want) != 7 || want[2] != revisions[0].Hash || want[6] != revisions[1].Hash {
			t.Fatalf("unexpected Git attribution: %v", want)
		}
		for _, backend := range []diff.Differ{
			xdiff.New(nil),
			xdiff.New(&xdiff.Options{Diff: native.Options{IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("(?s).*")}}}),
		} {
			result, err := git.BlameWithOptions(ctx, revisions[1], "functions.c",
				&git.BlameOptions{Differ: backend})
			if err != nil {
				t.Fatal(err)
			}
			var got []plumbing.Hash
			for _, line := range result.Lines {
				got = append(got, line.Hash)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("blame=%v, Git=%v", got, want)
			}
		}
	}
}
