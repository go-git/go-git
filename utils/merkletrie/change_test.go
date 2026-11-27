package merkletrie_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/internal/fsnoder"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type ChangeSuite struct {
	suite.Suite
}

func TestChangeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ChangeSuite))
}

func (s *ChangeSuite) TestActionString() {
	action := merkletrie.Insert
	s.Equal("Insert", action.String())

	action = merkletrie.Delete
	s.Equal("Delete", action.String())

	action = merkletrie.Modify
	s.Equal("Modify", action.String())
}

func (s *ChangeSuite) TestUnsupportedAction() {
	a := merkletrie.Action(42)
	s.Panics(func() { _ = a.String() })
}

func (s *ChangeSuite) TestEmptyChanges() {
	ret := merkletrie.NewChanges()
	p := noder.Path{}

	err := ret.AddRecursiveInsert(p)
	s.ErrorIs(err, merkletrie.ErrEmptyFileName)

	err = ret.AddRecursiveDelete(p)
	s.ErrorIs(err, merkletrie.ErrEmptyFileName)
}

func (s *ChangeSuite) TestNewInsert() {
	tree, err := fsnoder.New("(a(b(z<>)))")
	s.NoError(err)
	path := find(s.T(), tree, "z")
	change := merkletrie.NewInsert(path)
	s.Equal("<Insert a/b/z>", change.String())

	shortPath := noder.Path([]noder.Noder{path.Last()})
	change = merkletrie.NewInsert(shortPath)
	s.Equal("<Insert z>", change.String())
}

func (s *ChangeSuite) TestNewDelete() {
	tree, err := fsnoder.New("(a(b(z<>)))")
	s.NoError(err)
	path := find(s.T(), tree, "z")
	change := merkletrie.NewDelete(path)
	s.Equal("<Delete a/b/z>", change.String())

	shortPath := noder.Path([]noder.Noder{path.Last()})
	change = merkletrie.NewDelete(shortPath)
	s.Equal("<Delete z>", change.String())
}

func (s *ChangeSuite) TestNewModify() {
	tree1, err := fsnoder.New("(a(b(z<>)))")
	s.NoError(err)
	path1 := find(s.T(), tree1, "z")

	tree2, err := fsnoder.New("(a(b(z<1>)))")
	s.NoError(err)
	path2 := find(s.T(), tree2, "z")

	change := merkletrie.NewModify(path1, path2)
	s.Equal("<Modify a/b/z>", change.String())

	shortPath1 := noder.Path([]noder.Noder{path1.Last()})
	shortPath2 := noder.Path([]noder.Noder{path2.Last()})
	change = merkletrie.NewModify(shortPath1, shortPath2)
	s.Equal("<Modify z>", change.String())
}

func (s *ChangeSuite) TestMalformedChange() {
	change := merkletrie.Change{}
	s.PanicsWithError("malformed change: nil from and to", func() { _ = change.String() })
}

// collapsibleNoder wraps a noder and implements noder.Collapser, so tests
// can control whether a directory collapses into a single change.
type collapsibleNoder struct {
	noder.Noder
	collapse bool
}

func (n collapsibleNoder) Collapse() bool { return n.collapse }

func wrapLast(p noder.Path, collapse bool) noder.Path {
	wrapped := make(noder.Path, len(p))
	copy(wrapped, p)
	wrapped[len(wrapped)-1] = collapsibleNoder{Noder: p.Last(), collapse: collapse}
	return wrapped
}

func (s *ChangeSuite) TestAddRecursiveInsertCollapsedDir() {
	tree, err := fsnoder.New("(a(b(z<>)))")
	s.NoError(err)
	p := wrapLast(find(s.T(), tree, "b"), true)

	ret := merkletrie.NewChanges()
	err = ret.AddRecursiveInsert(p)
	s.NoError(err)

	s.Len(ret, 1)
	s.Equal("<Insert a/b>", ret[0].String())
}

func (s *ChangeSuite) TestAddRecursiveDeleteCollapsedDir() {
	tree, err := fsnoder.New("(a(b(z<>)))")
	s.NoError(err)
	p := wrapLast(find(s.T(), tree, "b"), true)

	ret := merkletrie.NewChanges()
	err = ret.AddRecursiveDelete(p)
	s.NoError(err)

	s.Len(ret, 1)
	s.Equal("<Delete a/b>", ret[0].String())
}

func (s *ChangeSuite) TestAddRecursiveInsertNonCollapsedDir() {
	tree, err := fsnoder.New("(a(b(z<>)))")
	s.NoError(err)
	p := wrapLast(find(s.T(), tree, "b"), false)

	ret := merkletrie.NewChanges()
	err = ret.AddRecursiveInsert(p)
	s.NoError(err)

	s.Len(ret, 1)
	s.Equal("<Insert a/b/z>", ret[0].String())
}
