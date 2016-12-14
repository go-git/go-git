package git

import (
	"sort"
	"testing"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type DiffTreeSuite struct {
	fixtures.Suite
	Storer  storer.EncodedObjectStorer
	Fixture *fixtures.Fixture
	cache   map[string]storer.EncodedObjectStorer
}

func (s *DiffTreeSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)
	s.Fixture = fixtures.Basic().One()
	sto, err := filesystem.NewStorage(s.Fixture.DotGit())
	c.Assert(err, IsNil)
	s.Storer = sto
	s.cache = make(map[string]storer.EncodedObjectStorer)
}

func (s *DiffTreeSuite) tree(c *C, h plumbing.Hash) *object.Tree {
	t, err := object.GetTree(s.Storer, h)
	c.Assert(err, IsNil)
	return t
}

func (s *DiffTreeSuite) commitFromStorer(c *C, sto storer.EncodedObjectStorer,
	h plumbing.Hash) *object.Commit {

	commit, err := object.GetCommit(sto, h)
	c.Assert(err, IsNil)
	return commit
}

func (s *DiffTreeSuite) storageFromPackfile(f *fixtures.Fixture) storer.EncodedObjectStorer {
	sto, ok := s.cache[f.URL]
	if ok {
		return sto
	}

	sto = memory.NewStorage()

	pf := f.Packfile()

	defer pf.Close()

	n := packfile.NewScanner(pf)
	d, err := packfile.NewDecoder(n, sto)
	if err != nil {
		panic(err)
	}

	_, err = d.Decode()
	if err != nil {
		panic(err)
	}

	s.cache[f.URL] = sto
	return sto
}

var _ = Suite(&DiffTreeSuite{})

func (s *DiffTreeSuite) TestActionString(c *C) {
	expected := "Insert"
	action := Insert
	obtained := action.String()
	c.Assert(obtained, Equals, expected)

	expected = "Delete"
	action = Delete
	obtained = action.String()
	c.Assert(obtained, Equals, expected)

	expected = "Modify"
	action = Modify
	obtained = action.String()
	c.Assert(obtained, Equals, expected)

	action = 37
	c.Assert(func() { _ = action.String() },
		PanicMatches, "unsupported action: 37")
}

func (s *DiffTreeSuite) TestChangeFilesInsert(c *C) {
	tree := s.tree(c, plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := &Change{Action: Insert}
	change.To.Name = "json/long.json"
	change.To.Tree = tree
	change.To.TreeEntry.Hash = plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")

	from, to, err := change.Files()
	c.Assert(err, IsNil)
	c.Assert(from, IsNil)
	c.Assert(to.ID(), Equals, change.To.TreeEntry.Hash)
}

func (s *DiffTreeSuite) TestChangeFilesDelete(c *C) {
	tree := s.tree(c, plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := &Change{Action: Delete}
	change.From.Name = "json/long.json"
	change.From.Tree = tree
	change.From.TreeEntry.Hash = plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")

	from, to, err := change.Files()
	c.Assert(err, IsNil)
	c.Assert(to, IsNil)
	c.Assert(from.ID(), Equals, change.From.TreeEntry.Hash)
}

func (s *DiffTreeSuite) TestChangeFilesModify(c *C) {
	tree := s.tree(c, plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))

	change := &Change{Action: Modify}
	change.To.Name = "json/long.json"
	change.To.Tree = tree
	change.To.TreeEntry.Hash = plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")
	change.From.Name = "json/long.json"
	change.From.Tree = tree
	change.From.TreeEntry.Hash = plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")

	from, to, err := change.Files()
	c.Assert(err, IsNil)
	c.Assert(to.ID(), Equals, change.To.TreeEntry.Hash)
	c.Assert(from.ID(), Equals, change.From.TreeEntry.Hash)
}

func (s *DiffTreeSuite) TestChangeString(c *C) {
	expected := "<Action: Insert, Path: foo>"
	change := &Change{Action: Insert}
	change.From.Name = "foo"

	obtained := change.String()
	c.Assert(obtained, Equals, expected)
}

func (s *DiffTreeSuite) TestChangesString(c *C) {
	expected := "[]"
	changes := newEmpty()
	obtained := changes.String()
	c.Assert(obtained, Equals, expected)

	expected = "[<Action: Modify, Path: bla>]"
	changes = make([]*Change, 1)
	changes[0] = &Change{Action: Modify}
	changes[0].From.Name = "bla"

	obtained = changes.String()
	c.Assert(obtained, Equals, expected)

	expected = "[<Action: Modify, Path: bla>, <Action: Insert, Path: foo/bar>]"
	changes = make([]*Change, 2)
	changes[0] = &Change{Action: Modify}
	changes[0].From.Name = "bla"
	changes[1] = &Change{Action: Insert}
	changes[1].From.Name = "foo/bar"
	obtained = changes.String()
	c.Assert(obtained, Equals, expected)
}

type expectChange struct {
	Action Action
	Name   string
}

func (s *DiffTreeSuite) TestDiffTree(c *C) {
	for i, t := range []struct {
		repository string         // the repo name as in localRepos
		commit1    string         // the commit of the first tree
		commit2    string         // the commit of the second tree
		expected   []expectChange // the expected list of []changeExpect
	}{{
		"https://github.com/dezfowler/LiteMock.git",
		"",
		"",
		[]expectChange{},
	}, {
		"https://github.com/dezfowler/LiteMock.git",
		"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
		"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
		[]expectChange{},
	}, {
		"https://github.com/dezfowler/LiteMock.git",
		"",
		"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
		[]expectChange{
			{Action: Insert, Name: "README"},
		},
	}, {
		"https://github.com/dezfowler/LiteMock.git",
		"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
		"",
		[]expectChange{
			{Action: Delete, Name: "README"},
		},
	}, {
		"https://github.com/githubtraining/example-branches.git",
		"",
		"f0eb272cc8f77803478c6748103a1450aa1abd37",
		[]expectChange{
			{Action: Insert, Name: "README.md"},
		},
	}, {
		"https://github.com/githubtraining/example-branches.git",
		"f0eb272cc8f77803478c6748103a1450aa1abd37",
		"",
		[]expectChange{
			{Action: Delete, Name: "README.md"},
		},
	}, {
		"https://github.com/githubtraining/example-branches.git",
		"f0eb272cc8f77803478c6748103a1450aa1abd37",
		"f0eb272cc8f77803478c6748103a1450aa1abd37",
		[]expectChange{},
	}, {
		"https://github.com/github/gem-builder.git",
		"",
		"9608eed92b3839b06ebf72d5043da547de10ce85",
		[]expectChange{
			{Action: Insert, Name: "README"},
			{Action: Insert, Name: "gem_builder.rb"},
			{Action: Insert, Name: "gem_eval.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"9608eed92b3839b06ebf72d5043da547de10ce85",
		"",
		[]expectChange{
			{Action: Delete, Name: "README"},
			{Action: Delete, Name: "gem_builder.rb"},
			{Action: Delete, Name: "gem_eval.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"9608eed92b3839b06ebf72d5043da547de10ce85",
		"9608eed92b3839b06ebf72d5043da547de10ce85",
		[]expectChange{},
	}, {
		"https://github.com/toqueteos/ts3.git",
		"",
		"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
		[]expectChange{
			{Action: Insert, Name: "README.markdown"},
			{Action: Insert, Name: "examples/bot.go"},
			{Action: Insert, Name: "examples/raw_shell.go"},
			{Action: Insert, Name: "helpers.go"},
			{Action: Insert, Name: "ts3.go"},
		},
	}, {
		"https://github.com/toqueteos/ts3.git",
		"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
		"",
		[]expectChange{
			{Action: Delete, Name: "README.markdown"},
			{Action: Delete, Name: "examples/bot.go"},
			{Action: Delete, Name: "examples/raw_shell.go"},
			{Action: Delete, Name: "helpers.go"},
			{Action: Delete, Name: "ts3.go"},
		},
	}, {
		"https://github.com/toqueteos/ts3.git",
		"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
		"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
		[]expectChange{},
	}, {
		"https://github.com/github/gem-builder.git",
		"9608eed92b3839b06ebf72d5043da547de10ce85",
		"6c41e05a17e19805879689414026eb4e279f7de0",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"6c41e05a17e19805879689414026eb4e279f7de0",
		"89be3aac2f178719c12953cc9eaa23441f8d9371",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
			{Action: Insert, Name: "gem_eval_test.rb"},
			{Action: Insert, Name: "security.rb"},
			{Action: Insert, Name: "security_test.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"89be3aac2f178719c12953cc9eaa23441f8d9371",
		"597240b7da22d03ad555328f15abc480b820acc0",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"597240b7da22d03ad555328f15abc480b820acc0",
		"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
			{Action: Modify, Name: "gem_eval_test.rb"},
			{Action: Insert, Name: "lazy_dir.rb"},
			{Action: Insert, Name: "lazy_dir_test.rb"},
			{Action: Modify, Name: "security.rb"},
			{Action: Modify, Name: "security_test.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
		"597240b7da22d03ad555328f15abc480b820acc0",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
			{Action: Modify, Name: "gem_eval_test.rb"},
			{Action: Delete, Name: "lazy_dir.rb"},
			{Action: Delete, Name: "lazy_dir_test.rb"},
			{Action: Modify, Name: "security.rb"},
			{Action: Modify, Name: "security_test.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
		"ca9fd470bacb6262eb4ca23ee48bb2f43711c1ff",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
			{Action: Modify, Name: "security.rb"},
			{Action: Modify, Name: "security_test.rb"},
		},
	}, {
		"https://github.com/github/gem-builder.git",
		"fe3c86745f887c23a0d38c85cfd87ca957312f86",
		"b7e3f636febf7a0cd3ab473b6d30081786d2c5b6",
		[]expectChange{
			{Action: Modify, Name: "gem_eval.rb"},
			{Action: Modify, Name: "gem_eval_test.rb"},
			{Action: Insert, Name: "git_mock"},
			{Action: Modify, Name: "lazy_dir.rb"},
			{Action: Modify, Name: "lazy_dir_test.rb"},
			{Action: Modify, Name: "security.rb"},
		},
	}, {
		"https://github.com/rumpkernel/rumprun-xen.git",
		"1831e47b0c6db750714cd0e4be97b5af17fb1eb0",
		"51d8515578ea0c88cc8fc1a057903675cf1fc16c",
		[]expectChange{
			{Action: Modify, Name: "Makefile"},
			{Action: Modify, Name: "netbsd_init.c"},
			{Action: Modify, Name: "rumphyper_stubs.c"},
			{Action: Delete, Name: "sysproxy.c"},
		},
	}, {
		"https://github.com/rumpkernel/rumprun-xen.git",
		"1831e47b0c6db750714cd0e4be97b5af17fb1eb0",
		"e13e678f7ee9badd01b120889e0ec5fdc8ae3802",
		[]expectChange{
			{Action: Modify, Name: "app-tools/rumprun"},
		},
	}} {
		f := fixtures.ByURL(t.repository).One()
		sto := s.storageFromPackfile(f)

		var tree1, tree2 *object.Tree
		var err error
		if t.commit1 != "" {
			tree1, err = s.commitFromStorer(c, sto,
				plumbing.NewHash(t.commit1)).Tree()
			c.Assert(err, IsNil,
				Commentf("subtest %d: unable to retrieve tree from commit %s and repo %s: %s", i, t.commit1, t.repository, err))
		}

		if t.commit2 != "" {
			tree2, err = s.commitFromStorer(c, sto,
				plumbing.NewHash(t.commit2)).Tree()
			c.Assert(err, IsNil,
				Commentf("subtest %d: unable to retrieve tree from commit %s and repo %s", i, t.commit2, t.repository, err))
		}

		obtained, err := DiffTree(tree1, tree2)
		c.Assert(err, IsNil,
			Commentf("subtest %d: unable to calculate difftree: %s", i, err))
		c.Assert(equalChanges(obtained, t.expected), Equals, true,
			Commentf("subtest:%d\nrepo=%s\ncommit1=%s\ncommit2=%s\nexpected=%s\nobtained=%s",
				i, t.repository, t.commit1, t.commit2, t.expected, obtained))

		assertChanges(obtained, c)
	}
}

func assertChanges(a Changes, c *C) {
	for _, changes := range a {
		switch changes.Action {
		case Insert:
			c.Assert(changes.From.Tree, IsNil)
			c.Assert(changes.To.Tree, NotNil)
		case Delete:
			c.Assert(changes.From.Tree, NotNil)
			c.Assert(changes.To.Tree, IsNil)
		case Modify:
			c.Assert(changes.From.Tree, NotNil)
			c.Assert(changes.To.Tree, NotNil)
		}
	}
}

func equalChanges(a Changes, b []expectChange) bool {
	if len(a) != len(b) {
		return false
	}

	sort.Sort(a)

	for i, va := range a {
		vb := b[i]
		if va.Action != vb.Action || va.name() != vb.Name {
			return false
		}
	}

	return true
}
