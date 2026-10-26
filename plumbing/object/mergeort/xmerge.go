package mergeort

import (
	"bytes"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// This file is a structural port of git's xdiff/xmerge.c (xdl_merge): two
// pairwise change scripts against the base are walked together into merge
// regions (take-side-1, take-side-2, conflict), touching or overlapping
// changes fuse, conflicts are refined by an inner diff of the two sides and
// near-conflicts re-fused, then the regions are rendered with git-compatible
// conflict markers. The line-diff engine is go-diff rather than xdiff, so
// hunk tie-breaking may differ from git in degenerate inputs, but the region
// semantics — which changes auto-apply, which conflict, marker shape, favor
// and diff3 styles, CRLF adaptation — follow xmerge.c.

// xdiff merge levels (xmerge.h). Merges run at ZEALOUS; diff3 style caps
// the level at EAGER because the shown base must stay in sync with the
// region spans (xdl_do_merge applies the same cap).
const (
	xmMinimal = iota
	xmEager
	xmZealous
)

// favor values mirror XDL_MERGE_FAVOR_*: 0 none, 1 ours, 2 theirs, 3 union.
const (
	xmFavorNone = iota
	xmFavorOurs
	xmFavorTheirs
	xmFavorUnion
)

const defaultMarkerSize = 7

// xmpOptions carries the merge-file level parameters (the xmparam_t
// equivalent, plus the labels ll_merge derives from branch names).
type xmpOptions struct {
	level      int
	favor      int
	styleDiff3 bool
	markerSize int
	ancestor   string
	file1      string
	file2      string
}

// xchange is one change group of a pairwise diff: base lines [i1,i1+chg1)
// replaced by side lines [i2,i2+chg2) — the xdchange_t equivalent.
type xchange struct {
	i1, chg1, i2, chg2 int
}

// xregion is a merge region — the xdmerge_t equivalent. mode: 0 conflict,
// bit 1 take side 1's postimage, bit 2 take side 2's, 4 sides identical
// (emit as context). i0/chg0 span the base lines and are only read for
// diff3 rendering, which never runs conflict refinement (matching xdiff,
// where refinement leaves i0/chg0 untouched).
type xregion struct {
	mode     int
	i0, chg0 int
	i1, chg1 int
	i2, chg2 int
}

// xdlMerge merges ours and theirs modifications of base, returning the
// merged content and whether it is conflict-free.
func xdlMerge(base, ours, theirs []byte, o xmpOptions) ([]byte, bool) {
	if bytes.Equal(ours, theirs) {
		return ours, true
	}
	if bytes.Equal(base, ours) {
		return theirs, true
	}
	if bytes.Equal(base, theirs) {
		return ours, true
	}

	baseL := splitLines(base)
	oursL := splitLines(ours)
	theirsL := splitLines(theirs)

	xscr1 := buildScript(baseL, oursL)
	xscr2 := buildScript(baseL, theirsL)

	// One side changed nothing: take the other side wholesale.
	if len(xscr1) == 0 {
		return theirs, true
	}
	if len(xscr2) == 0 {
		return ours, true
	}

	level := o.level
	if o.styleDiff3 && level > xmEager {
		level = xmEager
	}

	regions := mergeScripts(xscr1, xscr2, level, baseL, oursL, theirsL)
	if level >= xmZealous {
		regions = refineConflicts(regions, oursL, theirsL)
		regions = simplifyNonConflicts(regions)
	}
	return fillMergeBuffer(baseL, oursL, theirsL, regions, o)
}

// mergeScripts walks the two change scripts together building merge
// regions, fusing regions that touch or overlap on either side
// (xdl_do_merge's main loop plus xdl_append_merge).
func mergeScripts(xscr1, xscr2 []xchange, level int, baseL, oursL, theirsL []string) []xregion {
	var out []xregion
	appendMerge := func(mode, i0, chg0, i1, chg1, i2, chg2 int) {
		if n := len(out); n > 0 {
			m := &out[n-1]
			if i1 <= m.i1+m.chg1 || i2 <= m.i2+m.chg2 {
				if mode != m.mode {
					m.mode = 0
				}
				m.chg0 = i0 + chg0 - m.i0
				m.chg1 = i1 + chg1 - m.i1
				m.chg2 = i2 + chg2 - m.i2
				return
			}
		}
		out = append(out, xregion{mode: mode, i0: i0, chg0: chg0, i1: i1, chg1: chg1, i2: i2, chg2: chg2})
	}

	i, j := 0, 0
	for i < len(xscr1) && j < len(xscr2) {
		s1, s2 := xscr1[i], xscr2[j]

		switch {
		case s1.i1+s1.chg1 < s2.i1:
			// Side 1 change ends before side 2 starts: applies alone.
			appendMerge(1,
				s1.i1, s1.chg1,
				s1.i2, s1.chg2,
				s2.i2-s2.i1+s1.i1, s1.chg1)
			i++
		case s2.i1+s2.chg1 < s1.i1:
			appendMerge(2,
				s2.i1, s2.chg1,
				s1.i2-s1.i1+s2.i1, s2.chg1,
				s2.i2, s2.chg2)
			j++
		default:
			// Ranges touch or overlap. At above-minimal levels, the
			// same change on both sides applies once instead of
			// conflicting (xdl_merge_cmp_lines).
			if level == xmMinimal || s1.i1 != s2.i1 ||
				s1.chg1 != s2.chg1 || s1.chg2 != s2.chg2 ||
				!equalSpans(oursL, s1.i2, theirsL, s2.i2, s1.chg2) {
				off := s1.i1 - s2.i1
				ffo := off + s1.chg1 - s2.chg1
				i0, i1, i2 := s1.i1, s1.i2, s2.i2
				if off > 0 {
					i0 -= off
					i1 -= off
				} else {
					i2 += off
				}
				chg0 := s1.i1 + s1.chg1 - i0
				chg1 := s1.i2 + s1.chg2 - i1
				chg2 := s2.i2 + s2.chg2 - i2
				if ffo < 0 {
					chg0 -= ffo
					chg1 -= ffo
				} else {
					chg2 += ffo
				}
				appendMerge(0, i0, chg0, i1, chg1, i2, chg2)
			}
			e1 := s1.i1 + s1.chg1
			e2 := s2.i1 + s2.chg1
			if e1 >= e2 {
				j++
			}
			if e2 >= e1 {
				i++
			}
		}
	}
	for ; i < len(xscr1); i++ {
		s1 := xscr1[i]
		appendMerge(1,
			s1.i1, s1.chg1,
			s1.i2, s1.chg2,
			s1.i1+len(theirsL)-len(baseL), s1.chg1)
	}
	for ; j < len(xscr2); j++ {
		s2 := xscr2[j]
		appendMerge(2,
			s2.i1, s2.chg1,
			s2.i1+len(oursL)-len(baseL), s2.chg1,
			s2.i2, s2.chg2)
	}
	return out
}

func equalSpans(a []string, ai int, b []string, bi int, n int) bool {
	if ai+n > len(a) || bi+n > len(b) {
		return false
	}
	for k := 0; k < n; k++ {
		if a[ai+k] != b[bi+k] {
			return false
		}
	}
	return true
}

// refineConflicts shrinks conflicts by diffing the two sides' versions of
// the conflict span: inner change groups become smaller conflicts and the
// lines common to both sides fall back into context (xdl_refine_conflicts).
// Side-empty conflicts are left alone, and identical sides switch the
// region to context-only (mode 4).
func refineConflicts(regions []xregion, oursL, theirsL []string) []xregion {
	out := make([]xregion, 0, len(regions))
	for _, m := range regions {
		if m.mode != 0 || m.chg1 == 0 || m.chg2 == 0 {
			out = append(out, m)
			continue
		}
		inner := buildScript(oursL[m.i1:m.i1+m.chg1], theirsL[m.i2:m.i2+m.chg2])
		if len(inner) == 0 {
			// The changes are identical.
			m.mode = 4
			out = append(out, m)
			continue
		}
		for _, x := range inner {
			out = append(out, xregion{
				mode: 0,
				// i0/chg0 intentionally stale, as in xdiff: the
				// merge style never reads them and diff3 never
				// refines.
				i0: m.i0, chg0: m.chg0,
				i1: x.i1 + m.i1, chg1: x.chg1,
				i2: x.i2 + m.i2, chg2: x.chg2,
			})
		}
	}
	return out
}

// simplifyNonConflicts fuses conflicts separated by at most three context
// lines into one (xdl_simplify_non_conflicts at non-aggressive level).
func simplifyNonConflicts(regions []xregion) []xregion {
	if len(regions) == 0 {
		return regions
	}
	out := regions[:1]
	for _, m := range regions[1:] {
		last := &out[len(out)-1]
		begin := last.i1 + last.chg1
		end := m.i1
		if last.mode != 0 || m.mode != 0 || end-begin > 3 {
			out = append(out, m)
		} else {
			last.chg1 = m.i1 + m.chg1 - last.i1
			last.chg2 = m.i2 + m.chg2 - last.i2
		}
	}
	return out
}

// fillMergeBuffer renders the regions (xdl_fill_merge_buffer): context
// between regions comes from ours; conflicts emit marker-delimited hunks
// with an optional diff3 base section; favor rewrites conflicts into
// take-side output. The second return reports conflicts after favor
// resolution, matching xdl_cleanup_merge counting.
func fillMergeBuffer(baseL, oursL, theirsL []string, regions []xregion, o xmpOptions) ([]byte, bool) {
	var buf bytes.Buffer
	i := 0
	conflicts := 0
	marker := o.markerSize
	if marker <= 0 {
		marker = defaultMarkerSize
	}

	for _, m := range regions {
		if o.favor != xmFavorNone && m.mode == 0 {
			m.mode = o.favor
		}
		if m.mode == 0 {
			conflicts++
			writeLines(&buf, oursL[i:m.i1])
			needsCR := isCrNeeded(oursL, m.i1, theirsL, m.i2, baseL)
			writeMarker(&buf, marker, '<', o.file1, needsCR)
			writePostimage(&buf, oursL[m.i1:m.i1+m.chg1], needsCR)
			if o.styleDiff3 {
				writeMarker(&buf, marker, '|', o.ancestor, needsCR)
				writePostimage(&buf, baseL[m.i0:m.i0+m.chg0], needsCR)
			}
			writeMarker(&buf, marker, '=', "", needsCR)
			writePostimage(&buf, theirsL[m.i2:m.i2+m.chg2], needsCR)
			writeMarker(&buf, marker, '>', o.file2, needsCR)
		} else if m.mode&3 != 0 {
			writeLines(&buf, oursL[i:m.i1])
			if m.mode&1 != 0 {
				writeLines(&buf, oursL[m.i1:m.i1+m.chg1])
			}
			if m.mode&2 != 0 {
				writeLines(&buf, theirsL[m.i2:m.i2+m.chg2])
			}
		} else {
			// mode 4: sides identical; the lines flow into the next
			// region's context (or the tail) because i is unchanged.
			continue
		}
		i = m.i1 + m.chg1
	}
	writeLines(&buf, oursL[i:])
	return buf.Bytes(), conflicts == 0
}

// writePostimage emits a side's conflict body, adding the final newline the
// following marker needs when the side's last line lacks one
// (xdl_recs_copy's add_nl).
func writePostimage(buf *bytes.Buffer, lines []string, needsCR bool) {
	writeLines(buf, lines)
	if n := len(lines); n > 0 {
		if last := lines[n-1]; !strings.HasSuffix(last, "\n") {
			if needsCR {
				buf.WriteByte('\r')
			}
			buf.WriteByte('\n')
		}
	}
}

func writeMarker(buf *bytes.Buffer, size int, ch byte, label string, needsCR bool) {
	buf.WriteString(strings.Repeat(string(ch), size))
	if label != "" {
		buf.WriteByte(' ')
		buf.WriteString(label)
	}
	if needsCR {
		buf.WriteByte('\r')
	}
	buf.WriteByte('\n')
}

// isCrNeeded decides whether marker lines should end in CRLF, matching the
// postimage's preceding line endings on both sides and falling back to the
// base's first line (is_cr_needed + is_eol_crlf).
func isCrNeeded(oursL []string, i1 int, theirsL []string, i2 int, baseL []string) bool {
	needsCR := eolCRLF(oursL, atLeastZero(i1-1))
	if needsCR != 0 {
		needsCR = eolCRLF(theirsL, atLeastZero(i2-1))
	}
	if needsCR != 0 {
		needsCR = eolCRLF(baseL, 0)
	}
	return needsCR > 0
}

func atLeastZero(i int) int {
	if i < 0 {
		return 0
	}
	return i
}

// eolCRLF reports the line-ending style at index i: 1 CRLF, 0 LF-only,
// -1 undeterminable (empty file or sole line without eol), mirroring
// is_eol_crlf.
func eolCRLF(lines []string, i int) int {
	if len(lines) == 0 {
		return -1
	}
	if i < len(lines)-1 {
		// All lines before the last must end in LF.
		return crlfAtEnd(lines[i])
	}
	line := lines[i]
	if strings.HasSuffix(line, "\n") {
		return crlfAtEnd(line)
	}
	if i == 0 {
		return -1
	}
	return crlfAtEnd(lines[i-1])
}

func crlfAtEnd(line string) int {
	if strings.HasSuffix(line, "\r\n") {
		return 1
	}
	return 0
}

func writeLines(buf *bytes.Buffer, lines []string) {
	for _, l := range lines {
		buf.WriteString(l)
	}
}

// buildScript computes one side's change groups against the base via the
// diff engine: contiguous Delete+Insert runs collapse into single groups
// (xdl_do_diff + xdl_build_script). DiffLinesToRunes encodes every line of
// both texts into one rune vocabulary so the diff walks whole lines.
func buildScript(baseL, sideL []string) []xchange {
	dmp := diffmatchpatch.New()
	rb, rs, _ := dmp.DiffLinesToRunes(joinLines(baseL), joinLines(sideL))
	diffs := dmp.DiffMainRunes(rb, rs, false)

	var script []xchange
	bi, si := 0, 0
	cur := xchange{-1, -1, -1, -1}
	flush := func() {
		if cur.i1 >= 0 {
			script = append(script, cur)
			cur = xchange{-1, -1, -1, -1}
		}
	}
	for _, d := range diffs {
		n := len([]rune(d.Text))
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			bi += n
			si += n
		case diffmatchpatch.DiffDelete:
			if cur.i1 < 0 {
				cur = xchange{i1: bi, chg1: 0, i2: si, chg2: 0}
			}
			bi += n
		case diffmatchpatch.DiffInsert:
			if cur.i1 < 0 {
				cur = xchange{i1: bi, chg1: 0, i2: si, chg2: 0}
			}
			si += n
		}
		if cur.i1 >= 0 {
			cur.chg1, cur.chg2 = bi-cur.i1, si-cur.i2
		}
	}
	flush()
	return script
}

func joinLines(lines []string) string {
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l)
	}
	return b.String()
}

// splitLines keeps the trailing newline on each line; the last line may
// lack one — it is still a line.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			out = append(out, string(b[start:i+1]))
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}

// maxDiffSize mirrors MAX_XDIFF_SIZE: inputs above it merge as binary.
const maxDiffSize = 1024 * 1024 * 1023

// isBinary mirrors git's buffer_is_binary: a NUL byte in the first 8KiB.
func isBinary(b []byte) bool {
	if len(b) > 8192 {
		b = b[:8192]
	}
	return bytes.IndexByte(b, 0) >= 0
}
