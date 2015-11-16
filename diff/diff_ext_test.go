package diff_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"gopkg.in/src-d/go-git.v2/diff"
)

func Test(t *testing.T) { TestingT(t) }

type suiteCommon struct{}

var _ = Suite(&suiteCommon{})

var diffTests = [...]struct {
	src string // the src string to diff
	dst string // the dst string to diff
}{
	// equal inputs
	{"", ""},
	{"a", "a"},
	{"a\n", "a\n"},
	{"a\nb", "a\nb"},
	{"a\nb\n", "a\nb\n"},
	{"a\nb\nc", "a\nb\nc"},
	{"a\nb\nc\n", "a\nb\nc\n"},
	// missing '\n'
	{"", "\n"},
	{"\n", ""},
	{"a", "a\n"},
	{"a\n", "a"},
	{"a\nb", "a\nb"},
	{"a\nb\n", "a\nb\n"},
	{"a\nb\nc", "a\nb\nc"},
	{"a\nb\nc\n", "a\nb\nc\n"},
	// generic
	{"a\nbbbbb\n\tccc\ndd\n\tfffffffff\n", "bbbbb\n\tccc\n\tDD\n\tffff\n"},
}

func (s *suiteCommon) TestCountLines(c *C) {
	for i, t := range diffTests {
		diffs := diff.Do(t.src, t.dst)
		src := diff.Src(diffs)
		dst := diff.Dst(diffs)
		c.Assert(src, Equals, t.src, Commentf("subtest %d, src=%q, dst=%q, bad calculated src", i, t.src, t.dst))
		c.Assert(dst, Equals, t.dst, Commentf("subtest %d, src=%q, dst=%q, bad calculated dst", i, t.src, t.dst))
	}
}
