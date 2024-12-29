package diff_test

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/utils/diff"
	"github.com/stretchr/testify/suite"

	"github.com/sergi/go-diff/diffmatchpatch"
)

type suiteCommon struct {
	suite.Suite
}

func TestSuiteCommon(t *testing.T) {
	suite.Run(t, new(suiteCommon))
}

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

func (s *suiteCommon) TestAll() {
	for i, t := range diffTests {
		diffs := diff.Do(t.src, t.dst)
		src := diff.Src(diffs)
		dst := diff.Dst(diffs)
		s.Equal(t.src, src, fmt.Sprintf("subtest %d, src=%q, dst=%q, bad calculated src", i, t.src, t.dst))
		s.Equal(t.dst, dst, fmt.Sprintf("subtest %d, src=%q, dst=%q, bad calculated dst", i, t.src, t.dst))
	}
}

var doTests = [...]struct {
	src, dst string
	exp      []diffmatchpatch.Diff
}{
	{
		src: "",
		dst: "",
		exp: []diffmatchpatch.Diff{},
	},
	{
		src: "a",
		dst: "a",
		exp: []diffmatchpatch.Diff{
			{
				Type: 0,
				Text: "a",
			},
		},
	},
	{
		src: "",
		dst: "abc\ncba",
		exp: []diffmatchpatch.Diff{
			{
				Type: 1,
				Text: "abc\ncba",
			},
		},
	},
	{
		src: "abc\ncba",
		dst: "",
		exp: []diffmatchpatch.Diff{
			{
				Type: -1,
				Text: "abc\ncba",
			},
		},
	},
	{
		src: "abc\nbcd\ncde",
		dst: "000\nabc\n111\nBCD\n",
		exp: []diffmatchpatch.Diff{
			{Type: 1, Text: "000\n"},
			{Type: 0, Text: "abc\n"},
			{Type: -1, Text: "bcd\ncde"},
			{Type: 1, Text: "111\nBCD\n"},
		},
	},
	{
		src: "A\nB\nC\nD\nE\nF\nG\nH\nI\nJ\nK\nL\nM\nN\nÑ\nO\nP\nQ\nR\nS\nT\nU\nV\nW\nX\nY\nZ",
		dst: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\nY\nZ",
		exp: []diffmatchpatch.Diff{
			{Type: -1, Text: "A\n"},
			{Type: 0, Text: "B\nC\nD\nE\nF\nG\n"},
			{Type: -1, Text: "H\n"},
			{Type: 0, Text: "I\nJ\nK\nL\nM\nN\n"},
			{Type: -1, Text: "Ñ\n"},
			{Type: 0, Text: "O\nP\nQ\nR\nS\nT\n"},
			{Type: -1, Text: "U\n"},
			{Type: 0, Text: "V\nW\nX\nY\nZ"},
		},
	},
	{
		src: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\nY\nZ",
		dst: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\nY\n",
		exp: []diffmatchpatch.Diff{
			{Type: 0, Text: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\nY\n"},
			{Type: -1, Text: "Z"},
		},
	},
	{
		src: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\nY\nZ",
		dst: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\nY",
		exp: []diffmatchpatch.Diff{
			{Type: 0, Text: "B\nC\nD\nE\nF\nG\nI\nJ\nK\nL\nM\nN\nO\nP\nQ\nR\nS\nT\nV\nW\nX\n"},
			{Type: -1, Text: "Y\nZ"},
			{Type: 1, Text: "Y"},
		},
	},
}

func (s *suiteCommon) TestDo() {
	for i, t := range doTests {
		diffs := diff.Do(t.src, t.dst)
		s.Equal(t.exp, diffs, fmt.Sprintf("subtest %d", i))
	}
}
