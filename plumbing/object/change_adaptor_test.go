package object

import (
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/utils/merkletrie"
	"github.com/go-git/go-git/v5/utils/merkletrie/noder"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type ChangeAdaptorFixtureSuite struct {
	fixtures.Suite
}

type ChangeAdaptorSuite struct {
	suite.Suite
	ChangeAdaptorFixtureSuite
	Storer  storer.EncodedObjectStorer
	Fixture *fixtures.Fixture
}

func (s *ChangeAdaptorSuite) SetupSuite() {
	s.Fixture = fixtures.Basic().One()
	sto := filesystem.NewStorage(s.Fixture.DotGit(), cache.NewObjectLRUDefault())
	s.Storer = sto
}

func (s *ChangeAdaptorSuite) tree(h plumbing.Hash) *Tree {
	t, err := GetTree(s.Storer, h)
	s.NoError(err)
	return t
}

func TestChangeAdaptorSuite(t *testing.T) {
	suite.Run(t, new(ChangeAdaptorSuite))
}

// utility function to build Noders from a tree and an tree entry.
func newNoder(t *Tree, e TreeEntry) noder.Noder {
	return &treeNoder{
		parent: t,
		name:   e.Name,
		mode:   e.Mode,
		hash:   e.Hash,
	}
}

// utility function to build Paths
func newPath(nn ...noder.Noder) noder.Path { return noder.Path(nn) }

func (s *ChangeAdaptorSuite) TestTreeNoderHashHasMode() {
	hash := plumbing.NewHash("aaaa" + strings.Repeat("0", 36))
	mode := filemode.Regular

	treeNoder := &treeNoder{
		hash: hash,
		mode: mode,
	}

	expected := []byte{
		0xaa, 0xaa, 0x00, 0x00, // original hash is aaaa and 16 zeros
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	expected = append(expected, filemode.Regular.Bytes()...)

	s.Equal(expected, treeNoder.Hash())
}

func (s *ChangeAdaptorSuite) TestNewChangeInsert() {
	tree := &Tree{}
	entry := TreeEntry{
		Name: "name",
		Mode: filemode.FileMode(42),
		Hash: plumbing.NewHash("aaaaa"),
	}
	path := newPath(newNoder(tree, entry))

	expectedTo, err := newChangeEntry(path)
	s.NoError(err)

	src := merkletrie.Change{
		From: nil,
		To:   path,
	}

	obtained, err := newChange(src)
	s.NoError(err)
	action, err := obtained.Action()
	s.NoError(err)
	s.Equal(merkletrie.Insert, action)
	s.Equal(ChangeEntry{}, obtained.From)
	s.Equal(expectedTo, obtained.To)
}

func (s *ChangeAdaptorSuite) TestNewChangeDelete() {
	tree := &Tree{}
	entry := TreeEntry{
		Name: "name",
		Mode: filemode.FileMode(42),
		Hash: plumbing.NewHash("aaaaa"),
	}
	path := newPath(newNoder(tree, entry))

	expectedFrom, err := newChangeEntry(path)
	s.NoError(err)

	src := merkletrie.Change{
		From: path,
		To:   nil,
	}

	obtained, err := newChange(src)
	s.NoError(err)
	action, err := obtained.Action()
	s.NoError(err)
	s.Equal(merkletrie.Delete, action)
	s.Equal(expectedFrom, obtained.From)
	s.Equal(ChangeEntry{}, obtained.To)
}

func (s *ChangeAdaptorSuite) TestNewChangeModify() {
	treeA := &Tree{}
	entryA := TreeEntry{
		Name: "name",
		Mode: filemode.FileMode(42),
		Hash: plumbing.NewHash("aaaaa"),
	}
	pathA := newPath(newNoder(treeA, entryA))
	expectedFrom, err := newChangeEntry(pathA)
	s.NoError(err)

	treeB := &Tree{}
	entryB := TreeEntry{
		Name: "name",
		Mode: filemode.FileMode(42),
		Hash: plumbing.NewHash("bbbb"),
	}
	pathB := newPath(newNoder(treeB, entryB))
	expectedTo, err := newChangeEntry(pathB)
	s.NoError(err)

	src := merkletrie.Change{
		From: pathA,
		To:   pathB,
	}

	obtained, err := newChange(src)
	s.NoError(err)
	action, err := obtained.Action()
	s.NoError(err)
	s.Equal(merkletrie.Modify, action)
	s.Equal(expectedFrom, obtained.From)
	s.Equal(expectedTo, obtained.To)
}

func (s *ChangeAdaptorSuite) TestEmptyChangeFails() {
	change := &Change{
		From: empty,
		To:   empty,
	}
	_, err := change.Action()
	s.ErrorContains(err, "malformed change")

	_, _, err = change.Files()
	s.ErrorContains(err, "malformed change")

	str := change.String()
	s.Equal("malformed change", str)
}

type noderMock struct{ noder.Noder }

func (s *ChangeAdaptorSuite) TestNewChangeFailsWithChangesFromOtherNoders() {
	src := merkletrie.Change{
		From: newPath(noderMock{}),
		To:   nil,
	}
	_, err := newChange(src)
	s.Error(err)

	src = merkletrie.Change{
		From: nil,
		To:   newPath(noderMock{}),
	}
	_, err = newChange(src)
	s.Error(err)
}

func (s *ChangeAdaptorSuite) TestChangeStringFrom() {
	expected := "<Action: Delete, Path: foo>"
	change := Change{}
	change.From.Name = "foo"

	obtained := change.String()
	s.Equal(expected, obtained)
}

func (s *ChangeAdaptorSuite) TestChangeStringTo() {
	expected := "<Action: Insert, Path: foo>"
	change := Change{}
	change.To.Name = "foo"

	obtained := change.String()
	s.Equal(expected, obtained)
}

func (s *ChangeAdaptorSuite) TestChangeFilesInsert() {
	tree := s.tree(plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := Change{}
	change.To.Name = "json/long.json"
	change.To.Tree = tree
	change.To.TreeEntry.Mode = filemode.Regular
	change.To.TreeEntry.Hash = plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")

	from, to, err := change.Files()
	s.NoError(err)
	s.Nil(from)
	s.Equal(change.To.TreeEntry.Hash, to.ID())
}

func (s *ChangeAdaptorSuite) TestChangeFilesInsertNotFound() {
	tree := s.tree(plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := Change{}
	change.To.Name = "json/long.json"
	change.To.Tree = tree
	change.To.TreeEntry.Mode = filemode.Regular
	// there is no object for this hash
	change.To.TreeEntry.Hash = plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	_, _, err := change.Files()
	s.Error(err)
}

func (s *ChangeAdaptorSuite) TestChangeFilesDelete() {
	tree := s.tree(plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := Change{}
	change.From.Name = "json/long.json"
	change.From.Tree = tree
	change.From.TreeEntry.Mode = filemode.Regular
	change.From.TreeEntry.Hash = plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")

	from, to, err := change.Files()
	s.NoError(err)
	s.Nil(to)
	s.Equal(change.From.TreeEntry.Hash, from.ID())
}

func (s *ChangeAdaptorSuite) TestChangeFilesDeleteNotFound() {
	tree := s.tree(plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := Change{}
	change.From.Name = "json/long.json"
	change.From.Tree = tree
	change.From.TreeEntry.Mode = filemode.Regular
	// there is no object for this hash
	change.From.TreeEntry.Hash = plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	_, _, err := change.Files()
	s.Error(err)
}

func (s *ChangeAdaptorSuite) TestChangeFilesModify() {
	tree := s.tree(plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := Change{}
	change.To.Name = "json/long.json"
	change.To.Tree = tree
	change.To.TreeEntry.Mode = filemode.Regular
	change.To.TreeEntry.Hash = plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")
	change.From.Name = "json/long.json"
	change.From.Tree = tree
	change.From.TreeEntry.Mode = filemode.Regular
	change.From.TreeEntry.Hash = plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")

	from, to, err := change.Files()
	s.NoError(err)
	s.Equal(change.To.TreeEntry.Hash, to.ID())
	s.Equal(change.From.TreeEntry.Hash, from.ID())
}

func (s *ChangeAdaptorSuite) TestChangeEntryFailsWithOtherNoders() {
	path := noder.Path{noderMock{}}
	_, err := newChangeEntry(path)
	s.Error(err)
}

func (s *ChangeAdaptorSuite) TestChangeEntryFromNilIsZero() {
	obtained, err := newChangeEntry(nil)
	s.NoError(err)
	s.Equal(ChangeEntry{}, obtained)
}

func (s *ChangeAdaptorSuite) TestChangeEntryFromSortPath() {
	tree := &Tree{}
	entry := TreeEntry{
		Name: "name",
		Mode: filemode.FileMode(42),
		Hash: plumbing.NewHash("aaaaa"),
	}
	path := newPath(newNoder(tree, entry))

	obtained, err := newChangeEntry(path)
	s.NoError(err)

	s.Equal(entry.Name, obtained.Name)
	s.Equal(tree, obtained.Tree)
	s.Equal(entry, obtained.TreeEntry)
}

func (s *ChangeAdaptorSuite) TestChangeEntryFromLongPath() {
	treeA := &Tree{}
	entryA := TreeEntry{
		Name: "nameA",
		Mode: filemode.FileMode(42),
		Hash: plumbing.NewHash("aaaa"),
	}

	treeB := &Tree{}
	entryB := TreeEntry{
		Name: "nameB",
		Mode: filemode.FileMode(24),
		Hash: plumbing.NewHash("bbbb"),
	}

	path := newPath(
		newNoder(treeA, entryA),
		newNoder(treeB, entryB),
	)

	obtained, err := newChangeEntry(path)
	s.NoError(err)

	s.Equal(entryA.Name+"/"+entryB.Name, obtained.Name)
	s.Equal(treeB, obtained.Tree)
	s.Equal(entryB, obtained.TreeEntry)
}

func (s *ChangeAdaptorSuite) TestNewChangesEmpty() {
	expected := "[]"
	changes, err := newChanges(nil)
	s.NoError(err)
	obtained := changes.String()
	s.Equal(expected, obtained)

	expected = "[]"
	changes, err = newChanges(merkletrie.Changes{})
	s.NoError(err)
	obtained = changes.String()
	s.Equal(expected, obtained)
}

func (s *ChangeAdaptorSuite) TestNewChanges() {
	treeA := &Tree{}
	entryA := TreeEntry{Name: "nameA"}
	pathA := newPath(newNoder(treeA, entryA))
	changeA := merkletrie.Change{
		From: nil,
		To:   pathA,
	}

	treeB := &Tree{}
	entryB := TreeEntry{Name: "nameB"}
	pathB := newPath(newNoder(treeB, entryB))
	changeB := merkletrie.Change{
		From: pathB,
		To:   nil,
	}
	src := merkletrie.Changes{changeA, changeB}

	changes, err := newChanges(src)
	s.NoError(err)
	s.Len(changes, 2)
	action, err := changes[0].Action()
	s.NoError(err)
	s.Equal(merkletrie.Insert, action)
	s.Equal("nameA", changes[0].To.Name)
	action, err = changes[1].Action()
	s.NoError(err)
	s.Equal(merkletrie.Delete, action)
	s.Equal("nameB", changes[1].From.Name)
}

func (s *ChangeAdaptorSuite) TestNewChangesFailsWithOtherNoders() {
	change := merkletrie.Change{
		From: nil,
		To:   newPath(noderMock{}),
	}
	src := merkletrie.Changes{change}

	_, err := newChanges(src)
	s.Error(err)
}

func (s *ChangeAdaptorSuite) TestSortChanges() {
	c1 := &Change{}
	c1.To.Name = "1"

	c2 := &Change{}
	c2.From.Name = "2"
	c2.To.Name = "2"

	c3 := &Change{}
	c3.From.Name = "3"

	changes := Changes{c3, c1, c2}
	sort.Sort(changes)

	s.Equal("<Action: Insert, Path: 1>", changes[0].String())
	s.Equal("<Action: Modify, Path: 2>", changes[1].String())
	s.Equal("<Action: Delete, Path: 3>", changes[2].String())
}
