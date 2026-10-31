package mergeort

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// ---- in-memory fixture helpers ----

type builder struct {
	t *testing.T
	s *memory.Storage
}

func newBuilder(t *testing.T) *builder {
	return &builder{t: t, s: memory.NewStorage()}
}

func (b *builder) blob(data string) plumbing.Hash {
	obj := b.s.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(int64(len(data)))
	w, err := obj.Writer()
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		b.t.Fatal(err)
	}
	w.Close()
	h, err := b.s.SetEncodedObject(obj)
	if err != nil {
		b.t.Fatal(err)
	}
	return h
}

// tree builds a tree from a path→entry map; string values are regular
// files, entry values are used as-is (symlinks, submodules, ...).
func (b *builder) tree(files map[string]any) *object.Tree {
	type ent struct {
		hash plumbing.Hash
		mode filemode.FileMode
	}
	dirs := map[string]map[string]ent{}
	getDir := func(dir string) map[string]ent {
		if dirs[dir] == nil {
			dirs[dir] = map[string]ent{}
		}
		return dirs[dir]
	}
	for p, v := range files {
		var e ent
		switch x := v.(type) {
		case string:
			e = ent{hash: b.blob(x), mode: filemode.Regular}
		case object.TreeEntry:
			e = ent{hash: x.Hash, mode: x.Mode}
		default:
			b.t.Fatalf("bad entry %v", v)
		}
		dir, name := "", p
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			dir, name = p[:i], p[i+1:]
		}
		getDir(dir)[name] = e
		// Ensure every ancestor directory exists as a map so the child
		// dir entry lands somewhere.
		for d := dir; d != ""; d = d[:strings.LastIndexByte(d, '/')] {
			parent := ""
			if i := strings.LastIndexByte(d, '/'); i >= 0 {
				parent = d[:i]
			}
			_ = getDir(parent)
			_ = getDir(d)
			if parent == "" {
				break
			}
		}
	}

	memo := map[string]plumbing.Hash{}
	var write func(dir string) plumbing.Hash
	write = func(dir string) plumbing.Hash {
		if h, ok := memo[dir]; ok {
			return h
		}
		entries := make([]object.TreeEntry, 0, len(dirs[dir]))
		for name, e := range dirs[dir] {
			mode := e.mode
			entries = append(entries, object.TreeEntry{Name: name, Mode: mode, Hash: e.hash})
		}
		h, err := (&merger{s: b.s}).putTree(entries)
		if err != nil {
			b.t.Fatal(err)
		}
		memo[dir] = h
		return h
	}
	// Write children before parents so dir entries get inserted; write
	// does that implicitly by hashing on demand, so just walk from the
	// deepest paths upward inserting dir entries into parents.
	for dir := range dirs {
		if dir == "" {
			continue
		}
		parent := ""
		if i := strings.LastIndexByte(dir, '/'); i >= 0 {
			parent = dir[:i]
		}
		getDir(parent)[filepath.Base(dir)] = ent{hash: write(dir), mode: filemode.Dir}
	}
	root := write("")
	tr, err := object.GetTree(b.s, root)
	if err != nil {
		b.t.Fatal(err)
	}
	return tr
}

func (b *builder) commit(tree *object.Tree, parents ...*object.Commit) *object.Commit {
	c := &object.Commit{
		Author:    object.Signature{Name: "t", Email: "t@e", When: time.Now()},
		Committer: object.Signature{Name: "t", Email: "t@e", When: time.Now()},
		Message:   "m",
		TreeHash:  tree.Hash,
	}
	for _, p := range parents {
		c.ParentHashes = append(c.ParentHashes, p.Hash)
	}
	obj := b.s.NewEncodedObject()
	if err := c.Encode(obj); err != nil {
		b.t.Fatal(err)
	}
	h, err := b.s.SetEncodedObject(obj)
	if err != nil {
		b.t.Fatal(err)
	}
	pc, err := object.GetCommit(b.s, h)
	if err != nil {
		b.t.Fatal(err)
	}
	return pc
}

func mustMergeTrees(t *testing.T, b *builder, base, ours, theirs *object.Tree, opts *Options) *Result {
	t.Helper()
	r, err := MergeTrees(b.s, base, ours, theirs, opts)
	if err != nil {
		t.Fatalf("MergeTrees: %v", err)
	}
	return r
}

// treeFiles flattens a result tree into path→hash for assertions.
func treeFiles(t *testing.T, tr *object.Tree) map[string]plumbing.Hash {
	t.Helper()
	out := map[string]plumbing.Hash{}
	w := object.NewTreeWalker(tr, true, nil)
	defer w.Close()
	for {
		name, entry, err := w.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		out[name] = entry.Hash
	}
	return out
}

// ---- tree-level unit tests ----

func TestMergeTreesTrivial(t *testing.T) {
	b := newBuilder(t)
	base := b.tree(map[string]any{"a": "1\n", "b": "2\n"})
	ours := b.tree(map[string]any{"a": "ours\n", "b": "2\n"})
	theirs := b.tree(map[string]any{"a": "1\n", "b": "theirs\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, DefaultOptions())
	if !r.Clean || len(r.Conflicts) != 0 {
		t.Fatalf("expected clean merge, got %+v", r.Conflicts)
	}
	files := treeFiles(t, r.Tree)
	if files["a"].String() != b.blob("ours\n").String() ||
		files["b"].String() != b.blob("theirs\n").String() {
		t.Fatalf("bad merge result: %v", files)
	}
}

func TestMergeTreesConflictStages(t *testing.T) {
	b := newBuilder(t)
	base := b.tree(map[string]any{"f": "a\nb\nc\n"})
	ours := b.tree(map[string]any{"f": "a\nOURS\nc\n"})
	theirs := b.tree(map[string]any{"f": "a\nTHEIRS\nc\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, &Options{OurLabel: "o", TheirLabel: "t"})
	if r.Clean {
		t.Fatal("expected conflict")
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].Path != "f" {
		t.Fatalf("unexpected conflicts: %+v", r.Conflicts)
	}
	st := r.Conflicts[0].Stages
	if st[0] == nil || st[1] == nil || st[2] == nil {
		t.Fatalf("expected all three stages: %+v", st)
	}
	// The merged file carries markers.
	blobObj, err := object.GetBlob(b.s, treeFiles(t, r.Tree)["f"])
	if err != nil {
		t.Fatal(err)
	}
	rc, err := blobObj.Reader()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("merged blob should carry conflict markers: %q", data)
	}
}

func TestMergeTreesModifyDelete(t *testing.T) {
	b := newBuilder(t)
	base := b.tree(map[string]any{"f": "1\n", "g": "2\n"})
	ours := b.tree(map[string]any{"f": "ours\n", "g": "2\n"})
	theirs := b.tree(map[string]any{"g": "2\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, DefaultOptions())
	if r.Clean {
		t.Fatal("modify/delete must conflict")
	}
	files := treeFiles(t, r.Tree)
	if _, ok := files["f"]; !ok {
		t.Fatal("modifier's version should be left in tree")
	}
}

func TestMergeTreesDeleteBoth(t *testing.T) {
	b := newBuilder(t)
	base := b.tree(map[string]any{"f": "1\n", "g": "2\n"})
	ours := b.tree(map[string]any{"g": "2\n"})
	theirs := b.tree(map[string]any{"g": "2\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, DefaultOptions())
	if !r.Clean {
		t.Fatalf("delete/delete should be clean: %+v", r.Conflicts)
	}
	if files := treeFiles(t, r.Tree); len(files) != 1 {
		t.Fatalf("f should be gone: %v", files)
	}
}

func TestMergeTreesAddAdd(t *testing.T) {
	b := newBuilder(t)
	base := b.tree(map[string]any{})
	ours := b.tree(map[string]any{"f": "ours\n"})
	theirs := b.tree(map[string]any{"f": "theirs\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, DefaultOptions())
	if r.Clean {
		t.Fatal("add/add must conflict")
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].Path != "f" {
		t.Fatalf("unexpected: %+v", r.Conflicts)
	}
}

func TestMergeTreesRenameExact(t *testing.T) {
	b := newBuilder(t)
	fHash := b.blob("content\n")
	base := b.tree(map[string]any{"f": "content\n", "other": "x\n"})
	ours := b.tree(map[string]any{
		"g":     object.TreeEntry{Mode: filemode.Regular, Hash: fHash},
		"other": "x\n",
	})
	theirs := b.tree(map[string]any{"f": "content\n", "other": "changed\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, DefaultOptions())
	if !r.Clean {
		t.Fatalf("clean rename expected: %+v", r.Conflicts)
	}
	files := treeFiles(t, r.Tree)
	if _, ok := files["f"]; ok {
		t.Fatal("old name should be gone")
	}
	if files["g"].String() != fHash.String() {
		t.Fatal("renamed content should survive")
	}
}

func TestMergeTreesDirectoryFileConflict(t *testing.T) {
	b := newBuilder(t)
	base := b.tree(map[string]any{"keep": "k\n"})
	ours := b.tree(map[string]any{"keep": "k\n", "x": "file\n"})
	theirs := b.tree(map[string]any{"keep": "k\n", "x/y": "in-dir\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, &Options{OurLabel: "ours", TheirLabel: "theirs"})
	if r.Clean {
		t.Fatal("file/directory must conflict")
	}
	files := treeFiles(t, r.Tree)
	if _, ok := files["x/y"]; !ok {
		t.Fatal("directory should survive at x")
	}
	relocated, ok := files["x~ours"]
	if !ok {
		t.Fatalf("file should move to x~ours: %v", files)
	}
	_ = relocated
	if len(r.Conflicts) != 1 || r.Conflicts[0].Path != "x~ours" {
		t.Fatalf("conflict should be recorded at relocated path: %+v", r.Conflicts)
	}
}

func TestMergeTreesDistinctTypes(t *testing.T) {
	b := newBuilder(t)
	link := object.TreeEntry{Mode: filemode.Symlink, Hash: b.blob("target\n")}
	base := b.tree(map[string]any{"f": "a\nb\n"})
	ours := b.tree(map[string]any{"f": "a\nOURS\n"})
	theirs := b.tree(map[string]any{"f": link})

	r := mustMergeTrees(t, b, base, ours, theirs, &Options{OurLabel: "ours", TheirLabel: "theirs"})
	if r.Clean {
		t.Fatal("distinct types must conflict")
	}
	files := treeFiles(t, r.Tree)
	// Regular file relocates; symlink keeps the path.
	if _, ok := files["f~ours"]; !ok {
		t.Fatalf("regular file should relocate to f~ours: %v", files)
	}
	if files["f"].String() != link.Hash.String() {
		t.Fatal("symlink should keep original path")
	}
}

func TestMergeTreesModeChange(t *testing.T) {
	b := newBuilder(t)
	exe := object.TreeEntry{Mode: filemode.Executable, Hash: b.blob("a\nb\nc\nd\ne\nf\ng\nh\n")}
	base := b.tree(map[string]any{"f": "a\nb\nc\nd\ne\nf\ng\nh\n"})
	ours := b.tree(map[string]any{"f": exe})
	theirs := b.tree(map[string]any{"f": "a\nb\nC\nd\ne\nf\ng\nh\n"})

	r := mustMergeTrees(t, b, base, ours, theirs, DefaultOptions())
	if !r.Clean {
		t.Fatalf("mode change + content change should merge cleanly: %+v", r.Conflicts)
	}
	tr := r.Tree
	e, err := tr.FindEntry("f")
	if err != nil {
		t.Fatal(err)
	}
	if e.Mode != filemode.Executable {
		t.Fatalf("expected executable mode, got %v", e.Mode)
	}
}

func TestMergeCommitsUnrelatedHistories(t *testing.T) {
	b := newBuilder(t)
	c1 := b.commit(b.tree(map[string]any{"a": "1\n"}))
	c2 := b.commit(b.tree(map[string]any{"z": "26\n"}))

	r, err := MergeCommits(b.s, c1, c2, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean {
		t.Fatalf("unrelated adds should merge cleanly: %+v", r.Conflicts)
	}
	files := treeFiles(t, r.Tree)
	if _, ok := files["a"]; !ok {
		t.Fatalf("file a expected: %v", files)
	}
	if _, ok := files["z"]; !ok {
		t.Fatalf("file z expected: %v", files)
	}
}

func TestMergeCommitsCrissCrossVirtualBase(t *testing.T) {
	// Two merge bases (criss-cross): the bases fold into a virtual base
	// before the final three-way merge.
	b := newBuilder(t)
	c0 := b.commit(b.tree(map[string]any{"a": "a\n", "b": "b\n"}))
	c1 := b.commit(b.tree(map[string]any{"a": "a1\n", "b": "b\n"}), c0)
	c2 := b.commit(b.tree(map[string]any{"a": "a\n", "b": "b2\n"}), c0)
	// m1 merges c1+c2; m2 merges c2+c1 (parents swapped).
	m1 := b.commit(b.tree(map[string]any{"a": "a1\n", "b": "b2\n"}), c1, c2)
	m2 := b.commit(b.tree(map[string]any{"a": "a1\n", "b": "b2\n"}), c2, c1)
	ours := b.commit(b.tree(map[string]any{"a": "a1\n", "b": "b2\n", "c": "ours\n"}), m1)
	theirs := b.commit(b.tree(map[string]any{"a": "a1\n", "b": "b2\n", "d": "theirs\n"}), m2)

	bases, err := ours.MergeBase(theirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) != 2 {
		t.Fatalf("expected 2 merge bases, got %d", len(bases))
	}

	r, err := MergeCommits(b.s, ours, theirs, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean {
		t.Fatalf("criss-cross should merge cleanly here: %+v", r.Conflicts)
	}
	files := treeFiles(t, r.Tree)
	if _, ok := files["c"]; !ok {
		t.Fatalf("file c expected: %v", files)
	}
	if _, ok := files["d"]; !ok {
		t.Fatalf("file d expected: %v", files)
	}
}

// ---- git merge-tree oracle ----

// oracleRepo builds a real git repo for a scenario.
type oracleRepo struct {
	t   *testing.T
	dir string
	// oursRef/theirsRef name the merge sides for the case harness
	// (labels and expectations follow them).
	oursRef, theirsRef string
}

func newOracleRepo(t *testing.T) *oracleRepo {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	o := &oracleRepo{t: t, dir: dir}
	o.git("init", "-q", "-b", "main", ".")
	o.git("config", "user.name", "t")
	o.git("config", "user.email", "t@e")
	return o
}

func (o *oracleRepo) git(args ...string) string {
	o.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = o.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		o.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// gitExit runs git tolerating a non-zero exit, returning output and code.
func (o *oracleRepo) gitExit(args ...string) (string, int) {
	o.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = o.dir
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		o.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out), code
}

func (o *oracleRepo) write(path, content string) {
	o.t.Helper()
	full := filepath.Join(o.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		o.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		o.t.Fatal(err)
	}
}

func (o *oracleRepo) remove(path string) {
	o.t.Helper()
	if err := os.RemoveAll(filepath.Join(o.dir, filepath.FromSlash(path))); err != nil {
		o.t.Fatal(err)
	}
}

// hashObjectStdin writes a blob from stdin and returns its hash.
func (o *oracleRepo) hashObjectStdin(content string) string {
	o.t.Helper()
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = o.dir
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		o.t.Fatalf("git hash-object: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func (o *oracleRepo) snapshot(msg string) {
	o.git("add", "-A")
	o.git("commit", "-q", "--allow-empty", "-m", msg)
}

// compare runs git merge-tree --write-tree side1 side2 and matches the
// result against MergeCommits.
func (o *oracleRepo) compare(t *testing.T, opts *Options) {
	o.t.Helper()
	o.compareArgs(t, opts)
}

// compareArgs is compare with extra git arguments before merge-tree
// (e.g. "-c", "merge.conflictstyle=diff3").
func (o *oracleRepo) compareArgs(t *testing.T, opts *Options, gitArgs ...string) {
	o.t.Helper()
	args := append(gitArgs, "merge-tree", "--write-tree", "--allow-unrelated-histories", "side1", "side2")
	out, code := o.gitExit(args...)
	wantTree := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	wantClean := code == 0
	if code != 0 && code != 1 {
		t.Fatalf("merge-tree failed (%d): %s", code, out)
	}

	repo, err := gogit.PlainOpen(o.dir)
	if err != nil {
		t.Fatal(err)
	}
	c1 := o.commitOf("refs/heads/side1")
	c2 := o.commitOf("refs/heads/side2")
	if opts == nil {
		opts = &Options{OurLabel: "side1", TheirLabel: "side2"}
	}
	r, err := MergeCommits(repo.Storer, c1, c2, opts)
	if err != nil {
		t.Fatalf("MergeCommits: %v", err)
	}
	if r.Clean != wantClean {
		t.Fatalf("clean mismatch: got %v want %v (conflicts: %+v)\nmerge-tree:\n%s",
			r.Clean, wantClean, r.Conflicts, out)
	}
	if got := r.Tree.Hash.String(); got != wantTree {
		t.Fatalf("tree mismatch:\n got %s\nwant %s\nmerge-tree:\n%s", got, wantTree, out)
	}
}

func (o *oracleRepo) commitOf(ref string) *object.Commit {
	r, err := gogit.PlainOpen(o.dir)
	if err != nil {
		o.t.Fatal(err)
	}
	h, err := r.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		o.t.Fatal(err)
	}
	c, err := object.GetCommit(r.Storer, *h)
	if err != nil {
		o.t.Fatal(err)
	}
	return c
}

func TestOracleMergeTree(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func(o *oracleRepo)
	}{
		{"clean-disjoint", func(o *oracleRepo) {
			o.write("a", "1\n2\n3\n")
			o.write("b", "x\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "ONE\n2\n3\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("b", "x\ny\n")
			o.snapshot("s2")
		}},
		{"same-change", func(o *oracleRepo) {
			o.write("a", "1\n2\n3\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "1\nTWO\n3\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("a", "1\nTWO\n3\n")
			o.snapshot("s2")
		}},
		{"conflict-overlap", func(o *oracleRepo) {
			o.write("a", "1\n2\n3\n4\n5\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "1\nOURS\n3\n4\n5\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("a", "1\nTHEIRS\n3\n4\n5\n")
			o.snapshot("s2")
		}},
		{"clean-apart", func(o *oracleRepo) {
			o.write("a", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "ONE\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("a", "1\n2\n3\n4\n5\n6\n7\n8\n9\nTEN\n")
			o.snapshot("s2")
		}},
		{"conflict-adjacent", func(o *oracleRepo) {
			o.write("a", "1\n2\n3\n4\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "1\nX\n3\n4\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("a", "1\n2\nY\n4\n")
			o.snapshot("s2")
		}},
		{"conflict-multi-hunk", func(o *oracleRepo) {
			o.write("a", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "l1\nS1a\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nS1b\nl11\nl12\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("a", "l1\nS2a\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nS2b\nl11\nl12\n")
			o.snapshot("s2")
		}},
		{"add-add", func(o *oracleRepo) {
			o.write("base", "b\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("n", "from side1\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("n", "from side2\n")
			o.snapshot("s2")
		}},
		{"add-add-same", func(o *oracleRepo) {
			o.write("base", "b\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("n", "same\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("n", "same\n")
			o.snapshot("s2")
		}},
		{"modify-delete", func(o *oracleRepo) {
			o.write("a", "1\n2\n3\n")
			o.write("keep", "k\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("a", "1\nCHANGED\n3\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.remove("a")
			o.snapshot("s2")
		}},
		{"delete-both", func(o *oracleRepo) {
			o.write("a", "1\n")
			o.write("keep", "k\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.remove("a")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.remove("a")
			o.snapshot("s2")
		}},
		{"add-one-side", func(o *oracleRepo) {
			o.write("a", "1\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("new", "n\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.snapshot("s2")
		}},
		{"rename-exact-clean", func(o *oracleRepo) {
			o.write("f", "rename me\nsecond line\n")
			o.write("other", "x\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("other", "x\ny\n")
			o.snapshot("s2")
		}},
		{"rename-with-modify", func(o *oracleRepo) {
			o.write("f", "line 1\nline 2\nline 3\nline 4\nline 5\n")
			o.write("other", "x\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("f", "line 1\nCHANGED\nline 3\nline 4\nline 5\n")
			o.snapshot("s2")
		}},
		{"rename-delete", func(o *oracleRepo) {
			o.write("f", "content\n")
			o.write("other", "x\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.remove("f")
			o.snapshot("s2")
		}},
		{"rename-1to1", func(o *oracleRepo) {
			o.write("f", "content\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.git("mv", "f", "g")
			o.snapshot("s2")
		}},
		{"rename-1to2", func(o *oracleRepo) {
			o.write("f", "content\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g1")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.git("mv", "f", "g2")
			o.snapshot("s2")
		}},
		{"rename-add-collision", func(o *oracleRepo) {
			o.write("f", "renamed content\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("g", "added by side2\n")
			o.snapshot("s2")
		}},
		{"df-file-vs-dir", func(o *oracleRepo) {
			o.write("keep", "k\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("x", "file\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("x/y", "in dir\n")
			o.snapshot("s2")
		}},
		{"df-dir-replaced-by-file", func(o *oracleRepo) {
			o.write("x/y", "in dir\n")
			o.write("keep", "k\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.remove("x")
			o.write("x", "now a file\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("x/y", "modified\n")
			o.snapshot("s2")
		}},
		{"mode-change", func(o *oracleRepo) {
			o.write("f", "a\nb\nc\nd\ne\nf\ng\nh\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("update-index", "--chmod=+x", "f")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("f", "a\nb\nC\nd\ne\nf\ng\nh\n")
			o.snapshot("s2")
		}},
		{"symlink-vs-file", func(o *oracleRepo) {
			o.write("f", "a\nb\nc\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("f", "a\nOURS\nc\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			// Symlinks via the index, avoiding filesystem symlink
			// support (Windows).
			o.git("rm", "-q", "f")
			h := o.hashObjectStdin("target\n")
			o.git("update-index", "--add", "--cacheinfo", "120000,"+h+",f")
			o.git("commit", "-q", "-m", "s2")
		}},
		{"subdir-nested", func(o *oracleRepo) {
			o.write("d1/d2/a", "1\n2\n3\n")
			o.write("d1/d2/b", "x\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("d1/d2/a", "ONE\n2\n3\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("d1/d2/b", "x\ny\n")
			o.snapshot("s2")
		}},
		{"binary-conflict", func(o *oracleRepo) {
			o.write("bin", "a\x00b\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.write("bin", "a\x00B1\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("bin", "a\x00B2\n")
			o.snapshot("s2")
		}},
		{"rename-similarity", func(o *oracleRepo) {
			o.write("f", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
			o.write("other", "x\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.write("g", "l1\nCHANGED\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("other", "x\ny\n")
			o.snapshot("s2")
		}},
		{"rename-similarity-with-both-modify", func(o *oracleRepo) {
			o.write("f", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
			o.snapshot("base")
			o.git("checkout", "-q", "-b", "side1")
			o.git("mv", "f", "g")
			o.write("g", "S1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
			o.snapshot("s1")
			o.git("checkout", "-q", "-b", "side2", "main")
			o.write("f", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nS2\n")
			o.snapshot("s2")
		}},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			o := newOracleRepo(t)
			sc.setup(o)
			o.compare(t, nil)
		})
	}
}

func TestOracleDiff3Style(t *testing.T) {
	o := newOracleRepo(t)
	o.write("a", "1\n2\n3\n4\n5\n")
	o.snapshot("base")
	o.git("checkout", "-q", "-b", "side1")
	o.write("a", "1\nOURS\n3\n4\n5\n")
	o.snapshot("s1")
	o.git("checkout", "-q", "-b", "side2", "main")
	o.write("a", "1\nTHEIRS\n3\n4\n5\n")
	o.snapshot("s2")
	o.compareArgs(t, &Options{
		OurLabel:   "side1",
		TheirLabel: "side2",
		Style:      StyleDiff3,
	}, "-c", "merge.conflictstyle=diff3")
}

func TestOracleCrissCross(t *testing.T) {
	o := newOracleRepo(t)
	o.write("a", "a\n")
	o.write("b", "b\n")
	o.snapshot("base")

	o.git("checkout", "-q", "-b", "t1")
	o.write("a", "a1\n")
	o.snapshot("c1")

	o.git("checkout", "-q", "-b", "t2", "main")
	o.write("b", "b2\n")
	o.snapshot("c2")

	o.git("checkout", "-q", "t1")
	o.git("merge", "-q", "--no-edit", "t2")
	o.git("checkout", "-q", "t2")
	o.git("merge", "-q", "--no-edit", "t1")

	o.git("checkout", "-q", "-b", "side1", "t1")
	o.write("c", "from side1\n")
	o.snapshot("s1")
	o.git("checkout", "-q", "-b", "side2", "t2")
	o.write("d", "from side2\n")
	o.snapshot("s2")

	o.compare(t, nil)
}

func TestOracleUnrelatedHistories(t *testing.T) {
	o := newOracleRepo(t)
	o.write("a", "1\n")
	o.snapshot("base")
	o.git("checkout", "-q", "--orphan", "side1")
	o.git("rm", "-q", "--cached", "a")
	o.remove("a")
	o.write("b", "2\n")
	o.snapshot("s1")
	o.git("checkout", "-q", "--orphan", "side2", "main")
	o.git("rm", "-q", "--cached", "a")
	o.remove("a")
	o.write("c", "3\n")
	o.snapshot("s2")
	o.compare(t, nil)
}
