package object

import (
	"context"
	"sort"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/diff"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

type ChangeSuite struct {
	suite.Suite
	Storer  storer.EncodedObjectStorer
	Fixture *fixtures.Fixture
}

func (s *ChangeSuite) SetupSuite() {
	s.Fixture = fixtures.ByURL("https://github.com/src-d/go-git.git").
		ByTag(".git").One()
	sto := filesystem.NewStorage(s.Fixture.DotGit(), cache.NewObjectLRUDefault())
	s.Storer = sto
}

func (s *ChangeSuite) tree(h plumbing.Hash) *Tree {
	t, err := GetTree(s.Storer, h)
	s.NoError(err)
	return t
}

func TestChangeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ChangeSuite))
}

func (s *ChangeSuite) TestInsert() {
	// Commit a5078b19f08f63e7948abd0a5e2fb7d319d3a565 of the go-git
	// fixture inserted "examples/clone/main.go".
	//
	// On that commit, the "examples/clone" tree is
	//     6efca3ff41cab651332f9ebc0c96bb26be809615
	//
	// and the "examples/colone/main.go" is
	//     f95dc8f7923add1a8b9f72ecb1e8db1402de601a

	path := "examples/clone/main.go"
	name := "main.go"
	mode := filemode.Regular
	blob := plumbing.NewHash("f95dc8f7923add1a8b9f72ecb1e8db1402de601a")
	tree := plumbing.NewHash("6efca3ff41cab651332f9ebc0c96bb26be809615")

	change := &Change{
		From: empty,
		To: ChangeEntry{
			Name: path,
			Tree: s.tree(tree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: blob,
			},
		},
	}

	action, err := change.Action()
	s.NoError(err)
	s.Equal(merkletrie.Insert, action)

	from, to, err := change.Files()
	s.NoError(err)
	s.Nil(from)
	s.Equal(path, to.Name)
	s.Equal(blob, to.Hash)

	p, err := change.Patch()
	s.NoError(err)
	s.Equal(1, len(p.FilePatches()))
	s.Equal(1, len(p.FilePatches()[0].Chunks()))
	s.Equal(diff.Add, p.FilePatches()[0].Chunks()[0].Type())

	p, err = change.PatchContext(context.Background())
	s.NoError(err)
	s.Equal(1, len(p.FilePatches()))
	s.Equal(1, len(p.FilePatches()[0].Chunks()))
	s.Equal(diff.Add, p.FilePatches()[0].Chunks()[0].Type())

	str := change.String()
	s.Equal("<Action: Insert, Path: examples/clone/main.go>", str)
}

func (s *ChangeSuite) TestDelete() {
	// Commit f6011d65d57c2a866e231fc21a39cb618f86f9ea of the go-git
	// fixture deleted "utils/difftree/difftree.go".
	//
	// The parent of that commit is
	//     9b4a386db3d98a4362516a00ef3d04d4698c9bcd.
	//
	// On that parent commit, the "utils/difftree" tree is
	//     f3d11566401ce4b0808aab9dd6fad3d5abf1481a.
	//
	// and the "utils/difftree/difftree.go" is
	//     e2cb9a5719daf634d45a063112b4044ee81da13ea.

	path := "utils/difftree/difftree.go"
	name := "difftree.go"
	mode := filemode.Regular
	blob := plumbing.NewHash("e2cb9a5719daf634d45a063112b4044ee81da13e")
	tree := plumbing.NewHash("f3d11566401ce4b0808aab9dd6fad3d5abf1481a")

	change := &Change{
		From: ChangeEntry{
			Name: path,
			Tree: s.tree(tree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: blob,
			},
		},
		To: empty,
	}

	action, err := change.Action()
	s.NoError(err)
	s.Equal(merkletrie.Delete, action)

	from, to, err := change.Files()
	s.NoError(err)
	s.Nil(to)
	s.Equal(path, from.Name)
	s.Equal(blob, from.Hash)

	p, err := change.Patch()
	s.NoError(err)
	s.Equal(1, len(p.FilePatches()))
	s.Equal(1, len(p.FilePatches()[0].Chunks()))
	s.Equal(diff.Delete, p.FilePatches()[0].Chunks()[0].Type())

	p, err = change.PatchContext(context.Background())
	s.NoError(err)
	s.Equal(1, len(p.FilePatches()))
	s.Equal(1, len(p.FilePatches()[0].Chunks()))
	s.Equal(diff.Delete, p.FilePatches()[0].Chunks()[0].Type())

	str := change.String()
	s.Equal("<Action: Delete, Path: utils/difftree/difftree.go>", str)
}

func (s *ChangeSuite) TestModify() {
	// Commit 7beaad711378a4daafccc2c04bc46d36df2a0fd1 of the go-git
	// fixture modified "examples/latest/latest.go".
	// the "examples/latest" tree is
	//     b1f01b730b855c82431918cb338ad47ed558999b.
	// and "examples/latest/latest.go" is blob
	//     05f583ace3a9a078d8150905a53a4d82567f125f.
	//
	// The parent of that commit is
	//     337148ef6d751477796922ac127b416b8478fcc4.
	// the "examples/latest" tree is
	//     8b0af31d2544acb5c4f3816a602f11418cbd126e.
	// and "examples/latest/latest.go" is blob
	//     de927fad935d172929aacf20e71f3bf0b91dd6f9.

	path := "utils/difftree/difftree.go"
	name := "difftree.go"
	mode := filemode.Regular
	fromBlob := plumbing.NewHash("05f583ace3a9a078d8150905a53a4d82567f125f")
	fromTree := plumbing.NewHash("b1f01b730b855c82431918cb338ad47ed558999b")
	toBlob := plumbing.NewHash("de927fad935d172929aacf20e71f3bf0b91dd6f9")
	toTree := plumbing.NewHash("8b0af31d2544acb5c4f3816a602f11418cbd126e")

	change := &Change{
		From: ChangeEntry{
			Name: path,
			Tree: s.tree(fromTree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: fromBlob,
			},
		},
		To: ChangeEntry{
			Name: path,
			Tree: s.tree(toTree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: toBlob,
			},
		},
	}

	action, err := change.Action()
	s.NoError(err)
	s.Equal(merkletrie.Modify, action)

	from, to, err := change.Files()
	s.NoError(err)

	s.Equal(path, from.Name)
	s.Equal(fromBlob, from.Hash)
	s.Equal(path, to.Name)
	s.Equal(toBlob, to.Hash)

	p, err := change.Patch()
	s.NoError(err)
	s.Equal(1, len(p.FilePatches()))
	s.Equal(7, len(p.FilePatches()[0].Chunks()))
	s.Equal(diff.Equal, p.FilePatches()[0].Chunks()[0].Type())
	s.Equal(diff.Delete, p.FilePatches()[0].Chunks()[1].Type())
	s.Equal(diff.Add, p.FilePatches()[0].Chunks()[2].Type())
	s.Equal(diff.Equal, p.FilePatches()[0].Chunks()[3].Type())
	s.Equal(diff.Delete, p.FilePatches()[0].Chunks()[4].Type())
	s.Equal(diff.Add, p.FilePatches()[0].Chunks()[5].Type())
	s.Equal(diff.Equal, p.FilePatches()[0].Chunks()[6].Type())

	p, err = change.PatchContext(context.Background())
	s.NoError(err)
	s.Equal(1, len(p.FilePatches()))
	s.Equal(7, len(p.FilePatches()[0].Chunks()))
	s.Equal(diff.Equal, p.FilePatches()[0].Chunks()[0].Type())
	s.Equal(diff.Delete, p.FilePatches()[0].Chunks()[1].Type())
	s.Equal(diff.Add, p.FilePatches()[0].Chunks()[2].Type())
	s.Equal(diff.Equal, p.FilePatches()[0].Chunks()[3].Type())
	s.Equal(diff.Delete, p.FilePatches()[0].Chunks()[4].Type())
	s.Equal(diff.Add, p.FilePatches()[0].Chunks()[5].Type())
	s.Equal(diff.Equal, p.FilePatches()[0].Chunks()[6].Type())

	str := change.String()
	s.Equal("<Action: Modify, Path: utils/difftree/difftree.go>", str)
}

func (s *ChangeSuite) TestEmptyChangeFails() {
	change := &Change{}

	_, err := change.Action()
	s.ErrorContains(err, "malformed")

	_, _, err = change.Files()
	s.ErrorContains(err, "malformed")

	str := change.String()
	s.Equal("malformed change", str)
}

// test reproducing bug #317
func (s *ChangeSuite) TestNoFileFilemodes() {
	f := fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One()

	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	iter, err := sto.IterEncodedObjects(plumbing.AnyObject)
	s.NoError(err)
	var commits []*Commit
	iter.ForEach(func(o plumbing.EncodedObject) error {
		if o.Type() == plumbing.CommitObject {
			commit, err := GetCommit(sto, o.Hash())
			s.NoError(err)
			commits = append(commits, commit)
		}

		return nil
	})

	s.NotEqual(0, len(commits))

	var prev *Commit
	for _, commit := range commits {
		if prev == nil {
			prev = commit
			continue
		}
		tree, err := commit.Tree()
		s.NoError(err)
		prevTree, err := prev.Tree()
		s.NoError(err)
		changes, err := DiffTree(tree, prevTree)
		s.NoError(err)
		for _, change := range changes {
			_, _, err := change.Files()
			s.NoError(err)
		}

		prev = commit
	}
}

func (s *ChangeSuite) TestErrorsFindingChildsAreDetected() {
	// Commit 7beaad711378a4daafccc2c04bc46d36df2a0fd1 of the go-git
	// fixture modified "examples/latest/latest.go".
	// the "examples/latest" tree is
	//     b1f01b730b855c82431918cb338ad47ed558999b.
	// and "examples/latest/latest.go" is blob
	//     05f583ace3a9a078d8150905a53a4d82567f125f.
	//
	// The parent of that commit is
	//     337148ef6d751477796922ac127b416b8478fcc4.
	// the "examples/latest" tree is
	//     8b0af31d2544acb5c4f3816a602f11418cbd126e.
	// and "examples/latest/latest.go" is blob
	//     de927fad935d172929aacf20e71f3bf0b91dd6f9.

	path := "utils/difftree/difftree.go"
	name := "difftree.go"
	mode := filemode.Regular
	fromBlob := plumbing.NewHash("aaaa") // does not exists
	fromTree := plumbing.NewHash("b1f01b730b855c82431918cb338ad47ed558999b")
	toBlob := plumbing.NewHash("bbbb") // does not exists
	toTree := plumbing.NewHash("8b0af31d2544acb5c4f3816a602f11418cbd126e")

	change := &Change{
		From: ChangeEntry{
			Name: path,
			Tree: s.tree(fromTree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: fromBlob,
			},
		},
		To: ChangeEntry{},
	}

	_, _, err := change.Files()
	s.ErrorContains(err, "file not found")

	change = &Change{
		From: empty,
		To: ChangeEntry{
			Name: path,
			Tree: s.tree(toTree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: toBlob,
			},
		},
	}

	_, _, err = change.Files()
	s.ErrorContains(err, "file not found")
}

func (s *ChangeSuite) TestChangesString() {
	expected := "[]"
	changes := Changes{}
	obtained := changes.String()
	s.Equal(expected, obtained)

	expected = "[<Action: Modify, Path: bla>]"
	changes = make([]*Change, 1)
	changes[0] = &Change{}
	changes[0].From.Name = "bla"
	changes[0].To.Name = "bla"

	obtained = changes.String()
	s.Equal(expected, obtained)

	expected = "[<Action: Modify, Path: bla>, <Action: Delete, Path: foo/bar>]"
	changes = make([]*Change, 2)
	changes[0] = &Change{}
	changes[0].From.Name = "bla"
	changes[0].To.Name = "bla"
	changes[1] = &Change{}
	changes[1].From.Name = "foo/bar"
	obtained = changes.String()
	s.Equal(expected, obtained)
}

func (s *ChangeSuite) TestChangesSort() {
	changes := make(Changes, 3)
	changes[0] = &Change{}
	changes[0].From.Name = "z"
	changes[0].To.Name = "z"
	changes[1] = &Change{}
	changes[1].From.Name = "b/b"
	changes[2] = &Change{}
	changes[2].To.Name = "b/a"

	expected := "[<Action: Insert, Path: b/a>, " +
		"<Action: Delete, Path: b/b>, " +
		"<Action: Modify, Path: z>]"

	sort.Sort(changes)
	s.Equal(expected, changes.String())
}

func (s *ChangeSuite) TestCancel() {
	// Commit a5078b19f08f63e7948abd0a5e2fb7d319d3a565 of the go-git
	// fixture inserted "examples/clone/main.go".
	//
	// On that commit, the "examples/clone" tree is
	//     6efca3ff41cab651332f9ebc0c96bb26be809615
	//
	// and the "examples/clone/main.go" is
	//     f95dc8f7923add1a8b9f72ecb1e8db1402de601a

	path := "examples/clone/main.go"
	name := "main.go"
	mode := filemode.Regular
	blob := plumbing.NewHash("f95dc8f7923add1a8b9f72ecb1e8db1402de601a")
	tree := plumbing.NewHash("6efca3ff41cab651332f9ebc0c96bb26be809615")

	change := &Change{
		From: empty,
		To: ChangeEntry{
			Name: path,
			Tree: s.tree(tree),
			TreeEntry: TreeEntry{
				Name: name,
				Mode: mode,
				Hash: blob,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := change.PatchContext(ctx)
	s.Nil(p)
	s.ErrorContains(err, "operation canceled")
}
