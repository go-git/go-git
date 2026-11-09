package mergeort

import (
	"strings"
	"testing"
)

func mkOpts() xmpOptions {
	return xmpOptions{
		level:      xmZealous,
		markerSize: 7,
		ancestor:   "base",
		file1:      "ours",
		file2:      "theirs",
	}
}

func TestXdlMergeFastPaths(t *testing.T) {
	base := "a\nb\nc\n"
	ours := "a\nB\nc\n"
	theirs := "a\nb\nc\n"

	// base == theirs: take ours.
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if !clean || string(out) != ours {
		t.Fatalf("base==theirs should take ours: clean=%v out=%q", clean, out)
	}

	out, clean = xdlMerge([]byte(base), []byte(ours), []byte(ours), mkOpts())
	if !clean || string(out) != ours {
		t.Fatalf("ours==theirs should take ours: clean=%v out=%q", clean, out)
	}
}

func TestXdlMergeDisjointChanges(t *testing.T) {
	base := "1\n2\n3\n4\n5\n6\n7\n8\n"
	ours := "ONE\n2\n3\n4\n5\n6\n7\n8\n"
	theirs := "1\n2\n3\n4\n5\n6\n7\nEIGHT\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	want := "ONE\n2\n3\n4\n5\n6\n7\nEIGHT\n"
	if !clean || string(out) != want {
		t.Fatalf("disjoint changes should both apply: clean=%v out=%q", clean, out)
	}
}

func TestXdlMergeAdjacentChangesConflict(t *testing.T) {
	// Changes touching adjacent lines (no context line between) conflict,
	// matching xdl_merge (xscr1 end == xscr2 start overlaps).
	base := "1\n2\n3\n4\n"
	ours := "1\nX\n3\n4\n"
	theirs := "1\n2\nY\n4\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if clean {
		t.Fatalf("adjacent changes must conflict, got %q", out)
	}
	want := "1\n" +
		"<<<<<<< ours\n" +
		"X\n3\n" +
		"=======\n" +
		"2\nY\n" +
		">>>>>>> theirs\n" +
		"4\n"
	if string(out) != want {
		t.Fatalf("adjacent conflict output mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeOverlapConflict(t *testing.T) {
	base := "a\nb\nc\nd\ne\n"
	ours := "a\nb\nOURS\nd\ne\n"
	theirs := "a\nb\nTHEIRS\nd\ne\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if clean {
		t.Fatalf("overlapping changes must conflict")
	}
	want := "a\nb\n" +
		"<<<<<<< ours\n" +
		"OURS\n" +
		"=======\n" +
		"THEIRS\n" +
		">>>>>>> theirs\n" +
		"d\ne\n"
	if string(out) != want {
		t.Fatalf("conflict output mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeIdenticalChangeAppliesOnce(t *testing.T) {
	base := "a\nb\nc\n"
	ours := "a\nX\nc\n"
	theirs := "a\nX\nc\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if !clean || string(out) != ours {
		t.Fatalf("identical change should apply once: clean=%v out=%q", clean, out)
	}
}

func TestXdlMergeRefineFusesSmallGaps(t *testing.T) {
	// Both sides rewrite the same span and share a middle line: zealous
	// refinement splits the conflict, but simplify_non_conflicts fuses
	// conflicts separated by <=3 lines back together (git merge-file
	// verified behavior).
	base := "a\nb\nc\nd\ne\n"
	ours := "a\nO1\ncommon\nO3\ne\n"
	theirs := "a\nT1\ncommon\nT3\ne\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if clean {
		t.Fatalf("expected conflict")
	}
	want := "a\n" +
		"<<<<<<< ours\n" +
		"O1\ncommon\nO3\n" +
		"=======\n" +
		"T1\ncommon\nT3\n" +
		">>>>>>> theirs\n" +
		"e\n"
	if string(out) != want {
		t.Fatalf("refined output mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeRefineSplitsWideGaps(t *testing.T) {
	// With more than 3 common lines between the inner changes, the
	// refined conflicts stay separate (git merge-file verified).
	base := "a\nb\nc\nd\ne\nf\ng\nh\n"
	ours := "a\nO1\nc2\nd2\ne2\nf2\ng2\nO3\n"
	theirs := "a\nT1\nc2\nd2\ne2\nf2\ng2\nT3\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if clean {
		t.Fatalf("expected conflict")
	}
	want := "a\n" +
		"<<<<<<< ours\nO1\n=======\nT1\n>>>>>>> theirs\n" +
		"c2\nd2\ne2\nf2\ng2\n" +
		"<<<<<<< ours\nO3\n=======\nT3\n>>>>>>> theirs\n"
	if string(out) != want {
		t.Fatalf("refined split mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeFusesNearConflicts(t *testing.T) {
	// Two conflicts separated by <=3 context lines fuse into one.
	base := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	ours := "A\n2\n3\n4\n5\n6\nB\n8\n9\n10\n"
	theirs := "X\n2\n3\n4\n5\n6\nY\n8\n9\n10\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if clean {
		t.Fatalf("expected conflict")
	}
	// The 5 common lines between the two change sites exceed 3, so they
	// stay separate; verify separation.
	if got := strings.Count(string(out), "<<<<<<<"); got != 2 {
		t.Fatalf("expected 2 separate conflicts, got %d: %q", got, out)
	}
}

func TestXdlMergeDiff3Style(t *testing.T) {
	base := "a\nb\nc\n"
	ours := "a\nOURS\nc\n"
	theirs := "a\nTHEIRS\nc\n"
	o := mkOpts()
	o.styleDiff3 = true
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), o)
	if clean {
		t.Fatalf("expected conflict")
	}
	want := "a\n" +
		"<<<<<<< ours\n" +
		"OURS\n" +
		"||||||| base\n" +
		"b\n" +
		"=======\n" +
		"THEIRS\n" +
		">>>>>>> theirs\n" +
		"c\n"
	if string(out) != want {
		t.Fatalf("diff3 output mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeFavor(t *testing.T) {
	base := "a\nb\nc\n"
	ours := "a\nOURS\nc\n"
	theirs := "a\nTHEIRS\nc\n"

	o := mkOpts()
	o.favor = xmFavorOurs
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), o)
	if !clean || string(out) != ours {
		t.Fatalf("favor ours: clean=%v out=%q", clean, out)
	}

	o.favor = xmFavorTheirs
	out, clean = xdlMerge([]byte(base), []byte(ours), []byte(theirs), o)
	if !clean || string(out) != theirs {
		t.Fatalf("favor theirs: clean=%v out=%q", clean, out)
	}

	o.favor = xmFavorUnion
	out, clean = xdlMerge([]byte(base), []byte(ours), []byte(theirs), o)
	// Union favor rewrites conflicts into take-both output and, per
	// xdl_cleanup_merge counting after the favor mutation, resolves them.
	if !clean {
		t.Fatalf("union favor resolves conflicts per xdiff semantics")
	}
	want := "a\nOURS\nTHEIRS\nc\n"
	if string(out) != want {
		t.Fatalf("favor union output mismatch: %q", out)
	}
}

func TestXdlMergeMarkerSizeAndLabels(t *testing.T) {
	base := "a\n"
	ours := "O\n"
	theirs := "T\n"
	o := mkOpts()
	o.markerSize = 9
	o.file1 = ""
	o.file2 = ""
	out, _ := xdlMerge([]byte(base), []byte(ours), []byte(theirs), o)
	want := "<<<<<<<<<\nO\n=========\nT\n>>>>>>>>>\n"
	if string(out) != want {
		t.Fatalf("marker size output mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeNoTrailingNewline(t *testing.T) {
	base := "a\nb"
	ours := "a\nB"
	theirs := "a\nC"
	out, _ := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	want := "a\n<<<<<<< ours\nB\n=======\nC\n>>>>>>> theirs\n"
	if string(out) != want {
		t.Fatalf("no-trailing-newline mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeCRLF(t *testing.T) {
	base := "a\r\nb\r\n"
	ours := "a\r\nB\r\n"
	theirs := "a\r\nC\r\n"
	out, _ := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	want := "a\r\n<<<<<<< ours\r\nB\r\n=======\r\nC\r\n>>>>>>> theirs\r\n"
	if string(out) != want {
		t.Fatalf("CRLF mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestXdlMergeInsertionsSamePoint(t *testing.T) {
	// Both sides insert at the same spot with different content.
	base := "a\nb\n"
	ours := "a\nX\nb\n"
	theirs := "a\nY\nb\n"
	out, clean := xdlMerge([]byte(base), []byte(ours), []byte(theirs), mkOpts())
	if clean {
		t.Fatalf("same-point insertions must conflict: %q", out)
	}
	want := "a\n<<<<<<< ours\nX\n=======\nY\n>>>>>>> theirs\nb\n"
	if string(out) != want {
		t.Fatalf("insertion conflict mismatch:\nwant: %q\ngot:  %q", want, out)
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte("a\x00b")) {
		t.Fatal("NUL byte should be binary")
	}
	if isBinary([]byte("text\nonly\n")) {
		t.Fatal("plain text should not be binary")
	}
}
