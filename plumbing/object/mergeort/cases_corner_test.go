package mergeort

// Corner cases ported from git's t6400 (directory/file conflicts),
// t6405 (symlinks), t6401 (criss-cross merges), t6411 (file modes) and
// t6416 (recursive corner cases: virtual merge bases, submodule/symlink
// clashes, criss-cross renames).

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/filemode"
)

func TestPortedT6400(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			name:   "merge with d/f conflicts",
			origin: "t6400: Merge with d/f conflicts",
			setup: func(o *oracleRepo) {
				o.write("init", "Hello\n")
				o.commit("initial")
				o.branch("B")
				o.write("dir/foo", "foo\n")
				o.commit("File: dir/foo")
				o.checkout("B")
				o.write("dir", "file dir\n")
				o.commit("File: dir")
			},
			ours: "B", theirs: "main",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (file/directory): directory in the way of dir from B; moving it to dir~B instead.",
			},
			contains: map[string][]string{
				"dir/foo": {"foo\n"},
				"dir~B":   {"file dir\n"},
			},
		},
		{
			name:   "F/D conflict resolves cleanly",
			origin: "t6400: F/D conflict",
			setup: func(o *oracleRepo) {
				o.write("init", "Hello\n")
				o.commit("initial")
				o.write("before/one", "FILE\n")
				o.write("after", "FILE\n")
				o.commit("first")
				o.remove("after")
				o.mv("before", "after")
				o.commit("move")
				o.checkout("-b", "para", "HEAD^")
				o.write("another", "COMPLETELY ANOTHER FILE\n")
				o.commit("para")
			},
			ours: "para", theirs: "main",
			clean:      boolPtr(true),
			filesExact: map[string]string{"after/one": "FILE\n", "another": "COMPLETELY ANOTHER FILE\n"},
			filesGone:  []string{"before/one"},
		},
		{
			name:   "modify/delete + directory/file conflict",
			origin: "t6400: modify/delete + directory/file conflict",
			setup:  setupT6400Letters,
			ours:   "delete", theirs: "modify",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"letters~modify": {true, false, true},
			},
			filesExact: map[string]string{
				"letters.txt":  "<<<<<<< delete\nversion 1\n=======\nversion 2\n>>>>>>> modify\n",
				"letters/file": "",
			},
			contains: map[string][]string{
				"letters~modify": {"a\nb\nc\nd\ne\nf\ng\nh\ni\n"},
			},
		},
		{
			name:   "modify/delete + directory/file conflict, other way",
			origin: "t6400: modify/delete + directory/file conflict; other way",
			setup:  setupT6400Letters,
			ours:   "modify", theirs: "delete",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"letters~modify": {true, true, false},
			},
			filesExact: map[string]string{
				"letters.txt":  "<<<<<<< modify\nversion 2\n=======\nversion 1\n>>>>>>> delete\n",
				"letters/file": "",
			},
		},
	})
}

// setupT6400Letters builds the modify/delete + D/F letters history.
func setupT6400Letters(o *oracleRepo) {
	o.write("letters", "a\nb\nc\nd\ne\nf\ng\nh\n")
	o.commit("initial")
	o.appendFile("letters", "i\n")
	o.write("letters.txt", "version 2\n")
	o.commit("modified")
	o.branch("modify")
	o.checkout("-b", "delete", "HEAD^")
	o.rmIndexed("letters")
	o.write("letters/file", "")
	o.write("letters.txt", "version 1\n")
	o.commit("deleted")
}

func TestPortedT6405(t *testing.T) {
	setup := func(o *oracleRepo) {
		o.write("file", "")
		o.commit("initial")
		o.branch("b-symlink")
		o.branch("b-file")
		o.symlink("symlink", "file")
		o.commitIndex("main")
		o.checkout("b-symlink")
		o.remove("symlink")
		o.symlink("symlink", "file-different")
		o.commitIndex("b-symlink")
		o.checkout("b-file")
		o.remove("symlink")
		o.write("symlink", "plain-file\n")
		o.commit("b-file")
	}
	runMergeCases(t, []mergeCase{
		{
			name:   "symlink vs different symlink",
			origin: "t6405: merge main into b-symlink",
			setup:  setup,
			ours:   "b-symlink", theirs: "main",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (add/add): Merge conflict in symlink",
			},
			stages: map[string][3]bool{"symlink": {false, true, true}},
			modes:  map[string]filemode.FileMode{"symlink": filemode.Symlink},
		},
		{
			name:   "file vs symlink, merging into the file side",
			origin: "t6405: merge main into b-file",
			setup:  setup,
			ours:   "b-file", theirs: "main",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (distinct types)",
			},
			modes: map[string]filemode.FileMode{
				"symlink":        filemode.Symlink,
				"symlink~b-file": filemode.Regular,
			},
		},
		{
			name:   "file vs symlink, merging into the symlink side",
			origin: "t6405: merge b-file into main",
			setup:  setup,
			ours:   "main", theirs: "b-file",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (distinct types)",
			},
			modes: map[string]filemode.FileMode{
				"symlink":        filemode.Symlink,
				"symlink~b-file": filemode.Regular,
			},
		},
	})
}

func TestPortedT6401(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			name:   "criss-cross merge with virtual merge base",
			origin: "t6401: Criss-cross merge + result",
			setup: func(o *oracleRepo) {
				o.write("file", "1\n2\n3\n4\n5\n6\n7\n8\n9\n")
				o.commit("Initial commit")
				o.branch("A")
				o.branch("B")

				o.checkout("A")
				o.write("file", "1\n2\n3\n4\n5\n6\n7\n8 changed in B8, branch A\n9\n")
				o.commit("B8")

				o.checkout("B")
				o.write("file", "1\n2\n3 changed in C3, branch B\n4\n5\n6\n7\n8\n9\n")
				o.commit("C3")
				o.branch("C3")
				o.merge("-m", "pre E3 merge", "A")
				o.write("file", "1\n2\n3 changed in E3, branch B. New file size\n4\n5\n6\n7\n8 changed in B8, branch A\n9\n")
				o.commit("E3")

				o.checkout("A")
				o.merge("-m", "pre D8 merge", "C3")
				o.write("file", "1\n2\n3 changed in C3, branch B\n4\n5\n6\n7\n8 changed in D8, branch A. New file size 2\n9\n")
				o.commit("D8")
			},
			ours: "A", theirs: "B",
			clean: boolPtr(true),
			filesExact: map[string]string{
				"file": "1\n2\n3 changed in E3, branch B. New file size\n4\n5\n6\n7\n8 changed in D8, branch A. New file size 2\n9\n",
			},
		},
	})
}

func TestPortedT6411(t *testing.T) {
	setup := func(o *oracleRepo) {
		o.write("file1", "")
		o.commit("initial")
		o.checkout("-b", "a1")
		o.write("dummy", "")
		o.commit("a")
		o.checkout("-b", "b1")
		o.git("add", "file1")
		o.chmodx("file1")
		o.commitIndex("b1")

		o.checkout("-b", "a2", "main")
		o.write("file2", "")
		o.git("add", "file2")
		o.chmodx("file2")
		o.commitIndex("a2")
		o.checkout("-b", "b2", "main")
		o.write("file2", "")
		o.commit("b2")

		o.checkout("-b", "deletion", "main")
		o.rmIndexed("file1")
		o.commit("deletion")
	}
	runMergeCases(t, []mergeCase{
		{
			name:   "single mode change",
			origin: "t6411: resolve single mode change",
			setup:  setup,
			ours:   "a1", theirs: "b1",
			clean: boolPtr(true),
			modes: map[string]filemode.FileMode{"file1": filemode.Executable},
		},
		{
			name:   "double mode change conflicts",
			origin: "t6411: detect conflict on double mode change",
			setup:  setup,
			ours:   "a2", theirs: "b2",
			clean:  boolPtr(false),
			stages: map[string][3]bool{"file2": {false, true, true}},
			modes:  map[string]filemode.FileMode{"file2": filemode.Executable},
		},
		{
			name:   "delete/modechange conflict",
			origin: "t6411: detect delete/modechange conflict",
			setup:  setup,
			ours:   "b1", theirs: "deletion",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (modify/delete): file1 deleted in deletion and modified in b1.",
			},
			modes: map[string]filemode.FileMode{"file1": filemode.Executable},
		},
	})
}

// setupSubmoduleCommon builds an inner repository with three commits and
// stages its commits as gitlinks (gitlink entries merge without needing
// the submodule to be registered, matching t6416's shapes).
func setupSubmoduleCommon(o *oracleRepo) (c1, c2, c3 string) {
	o.git("init", "-q", "inner")
	o.git("-C", "inner", "config", "user.name", "t")
	o.git("-C", "inner", "config", "user.email", "t@e")
	o.git("-C", "inner", "commit", "-q", "--allow-empty", "-m", "c1")
	c1 = strings.TrimSpace(o.git("-C", "inner", "rev-parse", "HEAD"))
	o.git("-C", "inner", "commit", "-q", "--allow-empty", "-m", "c2")
	c2 = strings.TrimSpace(o.git("-C", "inner", "rev-parse", "HEAD"))
	o.git("-C", "inner", "commit", "-q", "--allow-empty", "-m", "c3")
	c3 = strings.TrimSpace(o.git("-C", "inner", "rev-parse", "HEAD"))
	return c1, c2, c3
}

// setupSubmoduleCrissCross mirrors t6416's submodule modify/modify: A has
// the submodule at c1, B moves it to c2, C to c3, D/E are -s ours merges.
func setupSubmoduleCrissCross(o *oracleRepo) {
	c1, c2, c3 := setupSubmoduleCommon(o)
	o.git("add", "inner") // keep the inner repo out of the outer tree
	o.commitIndex("warm")
	o.write("irrelevant-file", "")
	o.git("add", "irrelevant-file")
	o.gitlink("sub", c1)
	o.commitIndex("A")
	o.checkout("-b", "B", "main")
	o.gitlink("sub", c2)
	o.commitIndex("B")
	o.checkout("-b", "C", "main")
	o.gitlink("sub", c3)
	o.commitIndex("C")
	o.checkout("B^0")
	o.merge("-s", "ours", "-m", "D", "C")
	o.branch("D")
	o.checkout("C^0")
	o.merge("-s", "ours", "-m", "E", "B")
	o.branch("E")
}

// setupSubmoduleAddAdd mirrors t6416's submodule add/add: neither A nor the
// base has the submodule; B and C add different gitlink commits.
func setupSubmoduleAddAdd(o *oracleRepo) {
	_, c2, c3 := setupSubmoduleCommon(o)
	o.git("add", "inner")
	o.commitIndex("warm")
	o.write("irrelevant-file", "")
	o.git("add", "irrelevant-file")
	o.commitIndex("A")
	o.checkout("-b", "B", "main")
	o.gitlink("sub", c2)
	o.commitIndex("B")
	o.checkout("-b", "C", "main")
	o.gitlink("sub", c3)
	o.commitIndex("C")
	o.checkout("B^0")
	o.merge("-s", "ours", "-m", "D", "C")
	o.branch("D")
	o.checkout("C^0")
	o.merge("-s", "ours", "-m", "E", "B")
	o.branch("E")
}

func TestPortedT6416(t *testing.T) {
	runMergeCases(t, []mergeCase{
		{
			name:   "criss-cross rename with no modifications",
			origin: "t6416: merge simple rename+criss-cross with no modifications",
			setup: func(o *oracleRepo) {
				one := ""
				two := ""
				for i := 0; i < 10; i++ {
					one += "line " + itoa(i) + " in a sample file\n"
					two += "line " + itoa(i) + " in another sample file\n"
				}
				o.write("one", one)
				o.write("two", two)
				o.commit("initial")
				o.branch("L1")
				o.checkout("-b", "R1")
				o.mv("one", "three")
				o.commit("R1")
				o.checkout("L1")
				o.mv("two", "three")
				o.commit("L1")
				// Merge from detached HEADs so the -s ours merges
				// do not advance the merged branches (git's setups
				// use checkout <commit>^0 for the same reason).
				o.checkout("L1^0")
				o.merge("-s", "ours", "-m", "L2", "R1")
				o.branch("L2")
				o.checkout("R1^0")
				o.merge("-s", "ours", "-m", "R2", "L1")
				o.branch("R2")
			},
			ours: "L2", theirs: "R2",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"three": {true, true, true},
			},
			contains: map[string][]string{
				"three": {"in a sample file", "in another sample file"},
			},
		},
		{
			name:   "criss-cross rename merges with basic modification",
			origin: "t6416: merge criss-cross + rename merges with basic modification",
			setup: func(o *oracleRepo) {
				one := ""
				two := ""
				for i := 0; i < 10; i++ {
					one += "line " + itoa(i) + " in a sample file\n"
					two += "line " + itoa(i) + " in another sample file\n"
				}
				o.write("one", one)
				o.write("two", two)
				o.commit("initial")
				o.branch("L1")
				o.checkout("-b", "R1")
				o.mv("one", "three")
				o.appendFile("two", "more\n")
				o.commit("R1")
				o.checkout("L1")
				o.mv("two", "three")
				o.commit("L1")
				o.checkout("L1^0")
				o.merge("-s", "ours", "-m", "L2", "R1")
				o.branch("L2")
				o.checkout("R1^0")
				o.merge("-s", "ours", "-m", "R2", "L1")
				o.branch("R2")
			},
			ours: "L2", theirs: "R2",
			clean: boolPtr(false),
			stages: map[string][3]bool{
				"three": {true, true, true},
			},
			contains: map[string][]string{
				"three": {"in a sample file", "in another sample file"},
			},
		},
		{
			name:   "symlink modify/modify over criss-cross",
			origin: "t6416: check symlink modify/modify",
			setup: func(o *oracleRepo) {
				o.write("ignoreme", "")
				o.commit("A")
				o.symlink("fickle", "lagoon")
				o.commitIndex("A2")
				o.checkout("-b", "B", "main")
				o.remove("fickle")
				o.symlink("fickle", "disneyland")
				o.commitIndex("B")
				o.checkout("-b", "C", "main")
				o.remove("fickle")
				o.symlink("fickle", "home")
				o.commitIndex("C")
				o.checkout("B^0")
				o.merge("-s", "ours", "-m", "D", "C")
				o.branch("D")
				o.checkout("C^0")
				o.merge("-s", "ours", "-m", "E", "B")
				o.branch("E")
			},
			ours: "D", theirs: "E",
			clean:  boolPtr(false),
			stages: map[string][3]bool{"fickle": {true, true, true}},
			modes:  map[string]filemode.FileMode{"fickle": filemode.Symlink},
		},
		{
			name:   "symlink add/add over criss-cross",
			origin: "t6416: check symlink add/add",
			setup: func(o *oracleRepo) {
				o.write("ignoreme", "")
				o.commit("A")
				o.checkout("-b", "B", "main")
				o.symlink("fickle", "disneyland")
				o.commitIndex("B")
				o.checkout("-b", "C", "main")
				o.symlink("fickle", "home")
				o.commitIndex("C")
				o.checkout("B^0")
				o.merge("-s", "ours", "-m", "D", "C")
				o.branch("D")
				o.checkout("C^0")
				o.merge("-s", "ours", "-m", "E", "B")
				o.branch("E")
			},
			ours: "D", theirs: "E",
			clean:  boolPtr(false),
			stages: map[string][3]bool{"fickle": {false, true, true}},
		},
		{
			name:   "submodule modify/modify over criss-cross",
			origin: "t6416: check submodule modify/modify",
			setup:  setupSubmoduleCrissCross,
			ours:   "D", theirs: "E",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (submodule): Merge conflict in sub",
			},
			stages: map[string][3]bool{"sub": {true, true, true}},
		},
		{
			name:   "submodule add/add over criss-cross",
			origin: "t6416: check submodule add/add",
			setup:  setupSubmoduleAddAdd,
			ours:   "D", theirs: "E",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (submodule): Merge conflict in sub",
			},
			stages: map[string][3]bool{"sub": {false, true, true}},
		},
		{
			name:   "conflicting entry types: submodule vs symlink",
			origin: "t6416: check conflicting entry types (submodule vs symlink)",
			setup: func(o *oracleRepo) {
				// A nested repo supplies distinct commits for the
				// gitlink sides, like the original setup.
				o.git("init", "-q", "path")
				o.write("path/file-B", "")
				o.git("-C", "path", "add", "-A")
				o.git("-C", "path", "commit", "-q", "-m", "B")
				bCommit := strings.TrimSpace(o.git("-C", "path", "rev-parse", "HEAD"))

				o.write("irrelevant-file", "")
				// Stage explicitly: add -A would record the nested repo as a
				// gitlink already (git's setup adds only irrelevant-file).
				o.git("add", "irrelevant-file")
				o.commitIndex("A")
				o.checkout("-b", "B", "main")
				o.gitlink("path", bCommit)
				o.commitIndex("B")
				o.checkout("-b", "C", "main")
				o.remove("path")
				o.symlink("path", "irrelevant-file")
				o.commitIndex("C")
				o.checkout("B^0")
				o.merge("-s", "ours", "-m", "D", "C")
				o.branch("D")
				o.checkout("C^0")
				o.merge("-s", "ours", "-m", "E", "B")
				o.branch("E")
			},
			ours: "D", theirs: "E",
			clean: boolPtr(false),
			messages: []string{
				"CONFLICT (distinct types): path had different types on each side",
			},
		},
		{
			name:   "conflicting modes for regular file over criss-cross",
			origin: "t6416: check conflicting modes for regular file",
			skip:   "virtual-base fold order on equal-date bases decides which side's mode wins the folded base; go-git's MergeBase and git order ties differently (git marks the ideal conflict expectation as a known failure here), so the byte result is tie-dependent",
		},
	})
}
