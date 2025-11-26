package index

import (
	"bytes"
	"path"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type NoderSuite struct {
	suite.Suite
}

func TestNoderSuite(t *testing.T) {
	suite.Run(t, new(NoderSuite))
}

func (s *NoderSuite) TestDiff() {
	indexA := &index.Index{
		Entries: []*index.Entry{
			{Name: "foo", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "bar/foo", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "bar/qux", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "bar/baz/foo", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
		},
	}

	indexB := &index.Index{
		Entries: []*index.Entry{
			{Name: "foo", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "bar/foo", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "bar/qux", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "bar/baz/foo", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
		},
	}

	ch, err := merkletrie.DiffTree(NewRootNode(indexA), NewRootNode(indexB), isEquals)
	s.NoError(err)
	s.Len(ch, 0)
}

func (s *NoderSuite) TestDiffChange() {
	indexA := &index.Index{
		Entries: []*index.Entry{{
			Name: path.Join("bar", "baz", "bar"),
			Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
		}},
	}

	indexB := &index.Index{
		Entries: []*index.Entry{{
			Name: path.Join("bar", "baz", "foo"),
			Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
		}},
	}

	ch, err := merkletrie.DiffTree(NewRootNode(indexA), NewRootNode(indexB), isEquals)
	s.NoError(err)
	s.Len(ch, 2)
}

func (s *NoderSuite) TestDiffSkipIssue1455() {
	indexA := &index.Index{
		Entries: []*index.Entry{
			{
				Name:         path.Join("bar", "baz", "bar"),
				Hash:         plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
				SkipWorktree: true,
			},
			{
				Name:         path.Join("bar", "biz", "bat"),
				Hash:         plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
				SkipWorktree: false,
			},
		},
	}

	indexB := &index.Index{}

	ch, err := merkletrie.DiffTree(NewRootNode(indexB), NewRootNode(indexA), isEquals)
	s.NoError(err)
	s.Len(ch, 1)
	a, err := ch[0].Action()
	s.NoError(err)
	s.Equal(a, merkletrie.Insert)
}

func (s *NoderSuite) TestDiffDir() {
	indexA := &index.Index{
		Entries: []*index.Entry{{
			Name: "foo",
			Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
		}},
	}

	indexB := &index.Index{
		Entries: []*index.Entry{{
			Name: path.Join("foo", "bar"),
			Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
		}},
	}

	ch, err := merkletrie.DiffTree(NewRootNode(indexA), NewRootNode(indexB), isEquals)
	s.NoError(err)
	s.Len(ch, 2)
}

func (s *NoderSuite) TestDiffSameRoot() {
	indexA := &index.Index{
		Entries: []*index.Entry{
			{Name: "foo.go", Hash: plumbing.NewHash("aab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "foo/bar", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
		},
	}

	indexB := &index.Index{
		Entries: []*index.Entry{
			{Name: "foo/bar", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
			{Name: "foo.go", Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d")},
		},
	}

	ch, err := merkletrie.DiffTree(NewRootNode(indexA), NewRootNode(indexB), isEquals)
	s.NoError(err)
	s.Len(ch, 1)
}

func (s *NoderSuite) TestDiffFileMode() {
	indexA := &index.Index{
		Entries: []*index.Entry{{
			Name: "foo.bash",
			Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
			Mode: filemode.Executable,
		}},
	}

	indexB := &index.Index{
		Entries: []*index.Entry{{
			Name: "foo.bash",
			Hash: plumbing.NewHash("8ab686eafeb1f44702738c8b0f24f2567c36da6d"),
			Mode: filemode.Regular,
		}},
	}

	// filemode is false
	ch, err := merkletrie.DiffTree(
		NewRootNodeWithOptions(indexA, RootNodeOptions{}),
		NewRootNodeWithOptions(indexB, RootNodeOptions{}),
		isEquals)
	s.NoError(err)
	s.Len(ch, 0)

	// filemode is true
	ch, err = merkletrie.DiffTree(NewRootNode(indexA), NewRootNode(indexB), isEquals)
	s.NoError(err)
	s.Len(ch, 1)
}

var empty = make([]byte, 24)

func isEquals(a, b noder.Hasher) bool {
	if bytes.Equal(a.Hash(), empty) || bytes.Equal(b.Hash(), empty) {
		return false
	}

	return bytes.Equal(a.Hash(), b.Hash())
}
