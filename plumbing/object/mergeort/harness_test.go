package mergeort

// This file provides the merge-case harness, the go-git equivalent of
// git's t64xx merge test suites: each case scripts a repository history
// with real git (exactly like the shell tests it is ported from), then
// checks the in-core merge outcome on three levels:
//
//   - oracle parity: the result tree hash and cleanliness must equal
//     `git merge-tree --write-tree` on the same refs (byte-exact result,
//     the counterpart of git running every merge test under both the
//     recursive and ort algorithms via test_expect_merge_algorithm);
//   - content expectations: exact or substring file contents, absent
//     paths (ported from the shell tests' test_cmp/test_path_is_*);
//   - conflict expectations: recorded stages per path and CONFLICT (...)
//     message substrings (ported from test_grep and ls-files -u checks).
//
// Ported expectations translate git-merge label conventions to
// merge-tree's: `git merge X` on branch HEAD labels conflicts HEAD/X,
// while `git merge-tree HEAD X` labels them with the literal ref names,
// and relocations like path~HEAD become path~<ours-ref>.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// mergeCase is one ported merge test case.
type mergeCase struct {
	name   string
	origin string // e.g. "t6422: rename/rename(1to2)"
	setup  func(c *oracleRepo)
	ours   string // ref on "our" side of the merge (default "side1")
	theirs string // ref on "their" side (default "side2")
	skip   string // non-empty: skip with this reason (like git's test prerequisites)

	clean *bool // expected cleanliness (nil: whatever the oracle says)

	filesExact map[string]string            // path → exact expected content
	filesGone  []string                     // paths that must be absent
	contains   map[string][]string          // path → substrings the content must include
	modes      map[string]filemode.FileMode // path → expected entry mode
	messages   []string                     // substrings expected in Result.Messages
	noMessages []string                     // substrings that must not appear
	// stages: path → which stages must be recorded (base, ours, theirs).
	stages map[string][3]bool

	gitConfig []string // extra `git -c ...` args for the oracle merge-tree
}

func boolPtr(b bool) *bool { return &b }

// runCase executes one case end to end.
func runCase(t *testing.T, mc mergeCase) {
	t.Helper()
	if mc.skip != "" {
		t.Skip(mc.skip)
	}
	if _, err := lookGit(); err != nil {
		t.Skip("git not available")
	}

	o := newOracleRepo(t)
	o.oursRef, o.theirsRef = mc.ours, mc.theirs
	if o.oursRef == "" {
		o.oursRef = "side1"
	}
	if o.theirsRef == "" {
		o.theirsRef = "side2"
	}
	mc.setup(o)

	// Oracle: git merge-tree --write-tree.
	args := append(append([]string{}, mc.gitConfig...),
		"merge-tree", "--write-tree", "--allow-unrelated-histories", o.oursRef, o.theirsRef)
	out, code := o.gitExit(args...)
	wantTree := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	wantClean := code == 0
	if code != 0 && code != 1 {
		t.Fatalf("oracle merge-tree failed (%d): %s", code, out)
	}

	// mergeort on the same refs.
	repo, err := gogit.PlainOpen(o.dir)
	if err != nil {
		t.Fatal(err)
	}
	c1 := o.commitOf("refs/heads/" + o.oursRef)
	c2 := o.commitOf("refs/heads/" + o.theirsRef)
	r, err := MergeCommits(repo.Storer, c1, c2, &Options{
		OurLabel:   o.oursRef,
		TheirLabel: o.theirsRef,
	})
	if err != nil {
		t.Fatalf("MergeCommits: %v", err)
	}

	// Oracle parity — byte-exact tree and cleanliness.
	if r.Clean != wantClean {
		t.Errorf("clean mismatch: mergeort=%v git=%v (conflicts: %+v)\ngit output:\n%s",
			r.Clean, wantClean, r.Conflicts, out)
	}
	if got := r.Tree.Hash.String(); got != wantTree {
		t.Errorf("tree mismatch (oracle parity):\n mergeort %s\n     git %s\ngit output:\n%s",
			got, wantTree, out)
	}

	if mc.clean != nil && r.Clean != *mc.clean {
		t.Errorf("cleanliness: want %v got %v", *mc.clean, r.Clean)
	}

	// Content expectations.
	for path, want := range mc.filesExact {
		got, ok := resultFile(t, repo, r.Tree, path)
		if !ok {
			t.Errorf("file %q missing from result tree", path)
			continue
		}
		if got != want {
			t.Errorf("file %q content mismatch:\nwant %q\ngot  %q", path, want, got)
		}
	}
	for path, subs := range mc.contains {
		got, ok := resultFile(t, repo, r.Tree, path)
		if !ok {
			t.Errorf("file %q missing from result tree", path)
			continue
		}
		for _, sub := range subs {
			if !strings.Contains(got, sub) {
				t.Errorf("file %q should contain %q, got %q", path, sub, got)
			}
		}
	}
	for _, path := range mc.filesGone {
		if _, ok := resultFile(t, repo, r.Tree, path); ok {
			t.Errorf("path %q should be gone from result tree", path)
		}
	}
	for path, want := range mc.modes {
		e, err := r.Tree.FindEntry(path)
		if err != nil {
			t.Errorf("file %q missing from result tree (mode check)", path)
			continue
		}
		if e.Mode != want {
			t.Errorf("mode for %q: want %v got %v", path, want, e.Mode)
		}
	}

	// Message expectations.
	for _, sub := range mc.messages {
		found := false
		for _, m := range r.Messages {
			if strings.Contains(m.Text, sub) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected message containing %q; got %+v", sub, r.Messages)
		}
	}
	for _, sub := range mc.noMessages {
		for _, m := range r.Messages {
			if strings.Contains(m.Text, sub) {
				t.Errorf("message %q should not appear", sub)
			}
		}
	}

	// Stage expectations.
	for path, want := range mc.stages {
		var got [3]bool
		found := false
		for _, c := range r.Conflicts {
			if c.Path != path {
				continue
			}
			found = true
			for i := 0; i < 3; i++ {
				got[i] = c.Stages[i] != nil
			}
		}
		if !found {
			// A path with stages expected must be recorded as conflicted.
			t.Errorf("expected conflict entry for %q; got %+v", path, conflictPaths(r))
			continue
		}
		if got != want {
			t.Errorf("stages for %q: want %v got %v", path, want, got)
		}
	}
}

func conflictPaths(r *Result) []string {
	var out []string
	for _, c := range r.Conflicts {
		out = append(out, c.Path)
	}
	return out
}

// resultFile reads a path's content from a result tree. Submodule entries
// have no readable blob; their content is reported as the commit hash.
func resultFile(t *testing.T, repo *gogit.Repository, tree *object.Tree, path string) (string, bool) {
	t.Helper()
	e, err := tree.FindEntry(path)
	if err != nil {
		return "", false
	}
	if e.Mode == filemode.Submodule {
		return e.Hash.String(), true
	}
	f, err := tree.File(path)
	if err != nil {
		return "", false
	}
	rc, err := f.Reader()
	if err != nil {
		return "", false
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data), true
}

// runMergeCases registers every case as a subtest.
func runMergeCases(t *testing.T, cases []mergeCase) {
	for _, mc := range cases {
		mc := mc
		t.Run(mc.name, func(t *testing.T) {
			runCase(t, mc)
		})
	}
}

// ---- setup DSL helpers (extend oracleRepo with merge-test verbs) ----

// appendFile appends content to a file.
func (o *oracleRepo) appendFile(path, content string) {
	o.t.Helper()
	full := o.fullPath(path)
	old, err := readFileIfExists(full)
	if err != nil {
		o.t.Fatal(err)
	}
	o.write(path, string(old)+content)
}

// mv renames a tracked file.
func (o *oracleRepo) mv(from, to string) {
	o.t.Helper()
	o.git("mv", from, to)
}

// rmIndexed removes a file from index and disk.
func (o *oracleRepo) rmIndexed(paths ...string) {
	o.t.Helper()
	o.git(append([]string{"rm", "-q", "-f", "-r"}, paths...)...)
}

// chmodx marks a staged file executable (test_chmod +x equivalent).
func (o *oracleRepo) chmodx(path string) {
	o.t.Helper()
	o.git("update-index", "--chmod=+x", path)
}

// symlink stages a symlink without filesystem support (test_ln_s_add
// equivalent via the index, needed on Windows).
func (o *oracleRepo) symlink(path, target string) {
	o.t.Helper()
	h := o.hashObjectStdin(target)
	o.git("update-index", "--add", "--cacheinfo", "120000,"+h+","+path)
}

// gitlink stages a submodule-like gitlink entry without a real submodule
// repository (update-index --cacheinfo 160000).
func (o *oracleRepo) gitlink(path, commitish string) {
	o.t.Helper()
	h := strings.TrimSpace(o.git("rev-parse", commitish))
	o.git("update-index", "--add", "--cacheinfo", "160000,"+h+","+path)
}

// branch creates a branch at the current HEAD.
func (o *oracleRepo) branch(name string) {
	o.t.Helper()
	o.git("branch", name)
}

// checkout switches branches (-b to create).
func (o *oracleRepo) checkout(args ...string) {
	o.t.Helper()
	o.git(append([]string{"checkout", "-q"}, args...)...)
}

// commit stages everything and commits.
func (o *oracleRepo) commit(msg string) {
	o.t.Helper()
	o.git("add", "-A")
	o.git("commit", "-q", "--allow-empty", "-m", msg)
}

// commitIndex commits the index as-is (after update-index surgery, where
// `add -A` would undo the index edits).
func (o *oracleRepo) commitIndex(msg string) {
	o.t.Helper()
	o.git("commit", "-q", "--allow-empty", "-m", msg)
}

// merge runs a real merge in the setup history (for building criss-cross
// and other complex shapes); strategy args are optional.
func (o *oracleRepo) merge(args ...string) {
	o.t.Helper()
	o.git(append([]string{"merge", "-q", "--no-edit"}, args...)...)
}

// seq produces `seq first last` output ("1\n2\n...").
func seq(first, last int) string {
	var b strings.Builder
	for i := first; i <= last; i++ {
		b.WriteString(itoa(i))
		b.WriteByte('\n')
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for ; i > 0; i /= 10 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
	}
	return string(digits)
}

// lookGit reports whether a git binary is available.
func lookGit() (string, error) {
	return exec.LookPath("git")
}

// readFileIfExists reads a file, returning nil content when absent.
func readFileIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// fullPath resolves a repo-relative slash path inside the worktree.
func (o *oracleRepo) fullPath(path string) string {
	return filepath.Join(o.dir, filepath.FromSlash(path))
}
