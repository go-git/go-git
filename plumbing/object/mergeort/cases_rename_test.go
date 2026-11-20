package mergeort

// Rename-heavy cases ported from git's t6402 (merge-rename), t6425
// (rename/delete) and t6422 (rename corner cases). Setups reproduce the
// original histories with real git; the hard oracle-parity check against
// `git merge-tree --write-tree` pins the exact result trees, while the
// per-case expectations below carry over the shell tests' content and
// stage assertions (HEAD labels translated to the ours-ref name).

import (
	"strings"
	"testing"
)

// t6402 A/M fixtures: 15 lines "x xxxx...", line 7 (g/G) is what every
// branch edits.
func t6402File(lower bool) string {
	var b strings.Builder
	for i := 0; i < 15; i++ {
		ch := byte('a' + i)
		if !lower {
			ch = byte('A' + i)
		}
		b.WriteByte(ch)
		b.WriteByte(' ')
		b.WriteString(strings.Repeat(string(ch), 48))
		b.WriteByte('\n')
	}
	return b.String()
}

func t6402SetLine(content, newline string) string {
	lines := strings.Split(content, "\n")
	lines[6] = newline
	return strings.Join(lines, "\n")
}

// setupT6402 builds the t6402 base history: main (edits A), white/red/blue
// (rename A->B/C and M->N with per-branch edits), change and
// change+rename.
func setupT6402(o *oracleRepo) {
	a, m := t6402File(true), t6402File(false)
	o.write("A", a)
	o.write("M", m)
	o.commit("initial has A and M")
	o.branch("white")
	o.branch("red")
	o.branch("blue")
	o.branch("change")
	o.branch("change+rename")

	o.write("A", t6402SetLine(a, "g : main changes a line"))
	o.commit("main updates A")

	o.checkout("white")
	o.write("B", t6402SetLine(a, "g : white changes a line"))
	o.write("N", t6402SetLine(m, "G : colored branch changes a line"))
	o.rmIndexed("A", "M")
	o.commit("white renames A->B, M->N")

	o.checkout("red")
	o.write("B", t6402SetLine(a, "g : red changes a line"))
	o.write("N", t6402SetLine(m, "G : colored branch changes a line"))
	o.rmIndexed("A", "M")
	o.commit("red renames A->B, M->N")

	o.checkout("blue")
	o.write("C", t6402SetLine(a, "g : blue changes a line"))
	o.write("N", t6402SetLine(m, "G : colored branch changes a line"))
	o.rmIndexed("A", "M")
	o.commit("blue renames A->C, M->N")

	o.checkout("change")
	o.write("A", t6402SetLine(a, "g : changed line"))
	o.commit("changed")

	o.checkout("change+rename")
	o.write("B", t6402SetLine(a, "g : changed line"))
	o.rmIndexed("A")
	o.commit("changed and renamed")
}

func TestPortedT6402(t *testing.T) {
	nContent := t6402SetLine(t6402File(false), "G : colored branch changes a line")
	runMergeCases(t, []mergeCase{
		{
			name:   "rename into unrenaming one",
			origin: "t6402: pull renaming branch into unrenaming one",
			setup:  setupT6402,
			ours:   "main", theirs: "white",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"B": {true, true, true},
			},
			contains: map[string][]string{
				"B": {"g : main changes a line", "g : white changes a line"},
			},
			filesExact: map[string]string{"N": nContent},
		},
		{
			name:   "rename into another renaming one",
			origin: "t6402: pull renaming branch into another renaming one",
			setup:  setupT6402,
			ours:   "red", theirs: "white",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"B": {true, true, true},
			},
			contains: map[string][]string{
				"B": {"g : red changes a line", "g : white changes a line"},
			},
			filesExact: map[string]string{"N": nContent},
		},
		{
			name:   "unrenaming into renaming one",
			origin: "t6402: pull unrenaming branch into renaming one",
			setup:  setupT6402,
			ours:   "red", theirs: "main",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"B": {true, true, true},
			},
			contains: map[string][]string{
				"B": {"g : red changes a line", "g : main changes a line"},
			},
			filesExact: map[string]string{"N": nContent},
		},
		{
			name:   "conflicting renames (1to2)",
			origin: "t6402: pull conflicting renames",
			setup:  setupT6402,
			ours:   "red", theirs: "blue",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"A": {true, false, false},
				"B": {false, true, false},
				"C": {false, false, true},
			},
			contains: map[string][]string{
				"B": {"g : red changes a line"},
				"C": {"g : blue changes a line"},
			},
			filesExact: map[string]string{"N": nContent},
		},
		{
			name:   "identical changes in a renamed file",
			origin: "t6402: merge of identical changes in a renamed file",
			setup:  setupT6402,
			ours:   "change", theirs: "change+rename",
			clean:      boolPtr(true),
			filesGone:  []string{"A"},
			filesExact: map[string]string{"B": t6402SetLine(t6402File(true), "g : changed line")},
		},
		{
			name:   "rename plus small change",
			origin: "t6402: merge rename + small change",
			setup: func(o *oracleRepo) {
				o.write("file", "ORIGINAL\n")
				o.commit("Initial")
				o.checkout("-b", "rename_branch")
				o.mv("file", "renamed_file")
				o.commit("Rename")
				o.checkout("main")
				o.write("file", "NEW-VERSION\n")
				o.commit("Reformat")
			},
			ours: "main", theirs: "rename_branch",
			clean:      boolPtr(true),
			filesGone:  []string{"file"},
			filesExact: map[string]string{"renamed_file": "NEW-VERSION\n"},
		},
	})
}

// setupT6402RenameDF builds the rename+D/F histories from t6402.
func setupT6402RenameDF(o *oracleRepo) {
	o.write("sub/file", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	o.write("dir/file-in-the-way", "foo\n")
	o.commit("Common commit")
	firstBase := "dir-in-way~1"

	o.appendFile("sub/file", "11\n")
	o.appendFile("dir/file-in-the-way", "more\n")
	o.commit("Commit to merge, with dir in the way")
	o.branch("dir-in-way")

	// dir-not-in-way: the same sub/file change, without the directory.
	o.checkout("-b", "dir-not-in-way", firstBase)
	o.appendFile("sub/file", "11\n")
	o.rmIndexed("dir")
	o.commit("Commit to merge, with dir removed")

	// renamed-file-has-no-conflicts: sub/file renamed onto dir, with a
	// clean-merging line change.
	o.checkout("-b", "renamed-file-has-no-conflicts", firstBase)
	o.rmIndexed("dir")
	o.rmIndexed("sub/file")
	o.write("dir", "1\n2\n3\n4\n5555\n6\n7\n8\n9\n10\n")
	o.commit("Independent change")

	// renamed-file-has-conflicts: same rename, but the change conflicts
	// with the appended 11.
	o.checkout("-b", "renamed-file-has-conflicts", firstBase)
	o.rmIndexed("dir")
	o.git("mv", "sub/file", "dir")
	o.appendFile("dir", "12\n")
	o.commit("Conflicting change")
}

func TestPortedT6402RenameDF(t *testing.T) {
	mergedNoConflict := "1\n2\n3\n4\n5555\n6\n7\n8\n9\n10\n11\n"
	conflictAtDir := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n" +
		"<<<<<<< " + "renamed-file-has-conflicts" + ":dir\n" +
		"12\n" +
		"=======\n" +
		"11\n" +
		">>>>>>> " + "dir-not-in-way" + ":sub/file\n"
	conflictAtDirOther := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n" +
		"<<<<<<< " + "dir-in-way" + ":sub/file\n" +
		"11\n" +
		"=======\n" +
		"12\n" +
		">>>>>>> " + "renamed-file-has-conflicts" + ":dir\n"
	// Same conflict as conflictAtDir, but merged with dir-in-way, whose
	// sub/file also carries the appended 11 (label swaps accordingly).
	conflictAtDirInWay := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n" +
		"<<<<<<< " + "renamed-file-has-conflicts" + ":dir\n" +
		"12\n" +
		"=======\n" +
		"11\n" +
		">>>>>>> " + "dir-in-way" + ":sub/file\n"

	runMergeCases(t, []mergeCase{
		{
			name:   "renamed file merges, dir not in way",
			origin: "t6402: Rename+D/F conflict; renamed file merges + dir not in way",
			setup:  setupT6402RenameDF,
			ours:   "renamed-file-has-no-conflicts", theirs: "dir-not-in-way",
			clean:      boolPtr(true),
			filesExact: map[string]string{"dir": mergedNoConflict},
		},
		{
			name:   "renamed file merges, dir in way",
			origin: "t6402: Rename+D/F conflict; renamed file merges but dir in way",
			setup:  setupT6402RenameDF,
			ours:   "renamed-file-has-no-conflicts", theirs: "dir-in-way",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (modify/delete): dir/file-in-the-way",
				"Auto-merging dir",
				"moving it to dir~renamed-file-has-no-conflicts instead",
			},
			filesExact: map[string]string{
				"dir~renamed-file-has-no-conflicts": mergedNoConflict,
			},
			contains: map[string][]string{"dir/file-in-the-way": {"foo\nmore\n"}},
		},
		{
			name:   "renamed file merges, dir in way, merged other way",
			origin: "t6402: Same as previous, but merged other way",
			setup:  setupT6402RenameDF,
			ours:   "dir-in-way", theirs: "renamed-file-has-no-conflicts",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (modify/delete): dir/file-in-the-way",
				"moving it to dir~renamed-file-has-no-conflicts instead",
			},
			filesExact: map[string]string{"dir~renamed-file-has-no-conflicts": mergedNoConflict},
		},
		{
			name:   "renamed file cannot merge, dir not in way",
			origin: "t6402: Rename+D/F conflict; renamed file cannot merge, dir not in way",
			setup:  setupT6402RenameDF,
			ours:   "renamed-file-has-conflicts", theirs: "dir-not-in-way",
			clean:      boolPtr(false),
			filesExact: map[string]string{"dir": conflictAtDir},
		},
		{
			name:   "renamed file cannot merge, dir in the way",
			origin: "t6402: Rename+D/F conflict; renamed file cannot merge and dir in the way",
			setup:  setupT6402RenameDF,
			ours:   "renamed-file-has-conflicts", theirs: "dir-in-way",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (modify/delete): dir/file-in-the-way",
				"moving it to dir~renamed-file-has-conflicts instead",
			},
			filesExact: map[string]string{"dir~renamed-file-has-conflicts": conflictAtDirInWay},
		},
		{
			name:   "renamed file cannot merge, dir in the way, other way",
			origin: "t6402: Same as previous, but merged other way",
			setup:  setupT6402RenameDF,
			ours:   "dir-in-way", theirs: "renamed-file-has-conflicts",
			clean:      boolPtr(false),
			filesExact: map[string]string{"dir~renamed-file-has-conflicts": conflictAtDirOther},
		},
	})
}

func TestPortedT6425(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			name:   "rename/delete",
			origin: "t6425: rename/delete",
			setup: func(o *oracleRepo) {
				o.write("A", "foo\n")
				o.commit("initial")
				o.checkout("-b", "rename")
				o.mv("A", "B")
				o.commit("rename")
				o.checkout("main")
				o.rmIndexed("A")
				o.commit("delete")
			},
			ours: "main", theirs: "rename",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (rename/delete): A renamed to B in rename, but deleted in main.",
			},
			stages:     map[string][3]bool{"B": {true, false, true}},
			filesExact: map[string]string{"B": "foo\n"},
		},
	})
}

func TestPortedT6422(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			name:   "rename with content merge vs add",
			origin: "t6422: handle rename-with-content-merge vs. add",
			setup: func(o *oracleRepo) {
				o.write("a", seq(1, 5))
				o.commit("O")
				o.checkout("-b", "A")
				o.mv("a", "b")
				o.write("b", seq(0, 5))
				o.commit("A")
				o.checkout("-b", "B", "main")
				o.appendFile("a", "6\n")
				o.write("b", "hello world\n")
				o.commit("B")
			},
			ours: "A", theirs: "B",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (add/add): Merge conflict in b",
			},
			stages:    map[string][3]bool{"b": {false, true, true}},
			filesGone: []string{"a"},
			// :2:b is the two-way merge of A's renamed b and B's
			// appended a (0..6); :3:b keeps B's added file.
			contains: map[string][]string{"b": {"0\n1\n2\n3\n4\n5\n6\n"}},
		},
		{
			name:   "rename with content merge vs add, other way",
			origin: "t6422: handle rename-with-content-merge vs. add, merge other way",
			setup: func(o *oracleRepo) {
				o.write("a", seq(1, 5))
				o.commit("O")
				o.checkout("-b", "A")
				o.mv("a", "b")
				o.write("b", seq(0, 5))
				o.commit("A")
				o.checkout("-b", "B", "main")
				o.appendFile("a", "6\n")
				o.write("b", "hello world\n")
				o.commit("B")
			},
			ours: "B", theirs: "A",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (add/add): Merge conflict in b",
			},
			stages:    map[string][3]bool{"b": {false, true, true}},
			filesGone: []string{"a"},
			contains:  map[string][]string{"b": {"hello world\n", "0\n1\n2\n3\n4\n5\n"}},
		},
		{
			name:   "rename/rename (2to1)",
			origin: "t6422: handle rename/rename (2to1) conflict correctly",
			setup: func(o *oracleRepo) {
				o.write("a", "1\n2\n3\n4\n5\n")
				o.write("b", "5\n4\n3\n2\n1\n")
				o.commit("A")
				o.checkout("-b", "B", "main")
				o.mv("a", "c")
				o.appendFile("b", "0\n")
				o.commit("B")
				o.checkout("-b", "C", "main")
				o.mv("b", "c")
				o.appendFile("a", "6\n")
				o.commit("C")
			},
			ours: "B", theirs: "C",
			clean:     boolPtr(false),
			stages:    map[string][3]bool{"c": {false, true, true}},
			filesGone: []string{"a", "b"},
			// :2:c carries the merge of C's appended a into B's rename
			// target, :3:c the mirror for B's appended b.
			contains: map[string][]string{"c": {"6\n", "0\n"}},
		},
		{
			name:   "rename/rename (1to2)",
			origin: "t6422: merge has correct working tree contents",
			setup: func(o *oracleRepo) {
				o.write("a", "stuff\n")
				o.commit("A")
				o.checkout("-b", "B", "main")
				o.mv("a", "b")
				o.commit("B")
				o.checkout("-b", "C", "main")
				o.mv("a", "c")
				o.commit("C")
			},
			ours: "C", theirs: "B",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (rename/rename): a renamed to c in C and to b in B.",
			},
			stages: map[string][3]bool{
				"a": {true, false, false},
				"b": {false, false, true},
				"c": {false, true, false},
			},
			filesGone:  []string{"a"},
			filesExact: map[string]string{"b": "stuff\n", "c": "stuff\n"},
		},
		{
			name:   "rename/add/delete",
			origin: "t6422: rad-check: rename/add/delete conflict",
			setup: func(o *oracleRepo) {
				o.write("foo", "original file\n")
				o.commit("original")
				o.branch("O")
				o.branch("A")
				o.branch("B")
				o.checkout("A")
				o.rmIndexed("foo")
				o.write("bar", "different file\n")
				o.commit("Remove foo, add bar")
				o.checkout("B")
				o.mv("foo", "bar")
				o.commit("rename foo to bar")
			},
			ours: "B", theirs: "A",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (rename/delete): foo renamed to bar in B, but deleted in A.",
			},
			stages:    map[string][3]bool{"bar": {false, true, true}},
			filesGone: []string{"foo"},
			// bar is the two-way merge of B's renamed foo and A's
			// differently-added bar.
			contains: map[string][]string{"bar": {"original file\n", "different file\n"}},
		},
		{
			name:   "rename/rename(2to1)/delete/delete",
			origin: "t6422: rrdd-check: rename/rename(2to1)/delete/delete conflict",
			setup: func(o *oracleRepo) {
				o.write("foo", "foo\n")
				o.write("bar", "bar\n")
				o.commit("O")
				o.branch("O")
				o.branch("A")
				o.branch("B")
				o.checkout("A")
				o.mv("foo", "baz")
				o.rmIndexed("bar")
				o.commit("Rename foo, remove bar")
				o.checkout("B")
				o.mv("bar", "baz")
				o.rmIndexed("foo")
				o.commit("Rename bar, remove foo")
			},
			ours: "A", theirs: "B",
			clean:     boolPtr(false),
			stages:    map[string][3]bool{"baz": {false, true, true}},
			filesGone: []string{"foo", "bar"},
			contains:  map[string][]string{"baz": {"foo\n", "bar\n"}},
		},
		{
			name:   "nested conflicts from rename/rename(2to1)",
			origin: "t6422: check nested conflicts from rename/rename(2to1)",
			setup: func(o *oracleRepo) {
				base := ""
				for i := 1; i <= 10; i++ {
					base += "Random base content line " + itoa(i) + "\n"
				}
				v1 := base + "one\n"
				v2 := base + "uno\n"
				v3 := base + "eins\n"
				v4 := base + "two\n"
				v5 := base + "dos\n"
				v6 := base + "zwei\n"

				o.write("one", v1)
				o.write("two", v4)
				o.commit("english")
				o.branch("L")
				o.branch("R")

				o.checkout("L")
				o.rmIndexed("one", "two")
				o.write("three", v2)
				o.write("two", v5)
				o.commit("spanish")

				o.checkout("R")
				o.rmIndexed("one", "two")
				o.write("one", v3)
				o.write("three", v6)
				o.commit("german")
			},
			ours: "L", theirs: "R",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"three": {false, true, true},
			},
			filesGone: []string{"one", "two"},
			// three nests the 2to1 content merge inside a final
			// add/add-style merge: all six language markers appear.
			contains: map[string][]string{
				"three": {"uno\n", "eins\n", "dos\n", "zwei\n"},
			},
		},
		{
			name:   "rename/rename(1to2) with a binary file",
			origin: "t6422: rename/rename(1to2) with a binary file",
			skip:   "merge drivers/gitattributes are not supported: binary-by-attribute files text-merge here, diverging from git",
		},
		{
			name:   "rename/directory conflict + clean content merge",
			origin: "t6422: rename/directory conflict + clean content merge",
			setup: func(o *oracleRepo) {
				o.write("file", "1\n2\n3\n4\n5\n6\n")
				o.commit("base")
				o.checkout("-b", "right")
				o.appendFile("file", "7\n")
				o.write("newfile/realfile", "junk\n")
				o.commit("right")
				o.checkout("-b", "left-clean", "main")
				o.write("newfile", "0\n1\n2\n3\n4\n5\n6\n")
				o.rmIndexed("file")
				o.commit("left")
			},
			ours: "left-clean", theirs: "right",
			clean: boolPtr(false),
			messages: []string{
				"moving it to newfile~left-clean instead",
			},
			filesExact: map[string]string{
				"newfile~left-clean": "0\n1\n2\n3\n4\n5\n6\n7\n",
			},
			contains: map[string][]string{"newfile/realfile": {"junk\n"}},
		},
		{
			name:   "rename/directory conflict + content merge conflict",
			origin: "t6422: rename/directory conflict + content merge conflict",
			setup: func(o *oracleRepo) {
				o.write("file", "1\n2\n3\n4\n5\n6\n")
				o.commit("base")
				o.checkout("-b", "right")
				o.appendFile("file", "7\n")
				o.write("newfile/realfile", "junk\n")
				o.commit("right")
				o.checkout("-b", "left-conflict", "main")
				o.appendFile("file", "8\n")
				o.git("mv", "file", "newfile")
				o.commit("left")
			},
			ours: "left-conflict", theirs: "right",
			clean: boolPtr(false),
			messages: []string{
				"moving it to newfile~left-conflict instead",
			},
			stages: map[string][3]bool{
				"newfile~left-conflict": {true, true, true},
			},
			contains: map[string][]string{
				"newfile~left-conflict": {"<<<<<<<", "7\n", "8\n", ">>>>>>>"},
			},
		},
	})
}
