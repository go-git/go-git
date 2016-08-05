package difftree

import (
	"os"
	"sort"
	"testing"

	"gopkg.in/src-d/go-git.v3"
	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/formats/packfile"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type DiffTreeSuite struct {
	repos map[string]*git.Repository
}

var _ = Suite(&DiffTreeSuite{})

func (s *DiffTreeSuite) SetUpSuite(c *C) {
	fixtureRepos := [...]struct {
		url      string
		packfile string
	}{
		{"git://github.com/github/gem-builder.git",
			"fixtures/pack-1ea0b3971fd64fdcdf3282bfb58e8cf10095e4e6.pack"},
		{"git://github.com/githubtraining/example-branches.git",
			"fixtures/pack-bb8ee94710d3fa39379a630f76812c187217b312.pack"},
		{"git://github.com/rumpkernel/rumprun-xen.git",
			"fixtures/pack-7861f2632868833a35fe5e4ab94f99638ec5129b.pack"},
		{"git://github.com/mcuadros/skeetr.git",
			"fixtures/pack-36ef7a2296bfd526020340d27c5e1faa805d8d38.pack"},
		{"git://github.com/dezfowler/LiteMock.git",
			"fixtures/pack-0d9b6cfc261785837939aaede5986d7a7c212518.pack"},
		{"git://github.com/tyba/storable.git",
			"fixtures/pack-0d3d824fb5c930e7e7e1f0f399f2976847d31fd3.pack"},
		{"git://github.com/toqueteos/ts3.git",
			"fixtures/pack-21b33a26eb7ffbd35261149fe5d886b9debab7cb.pack"},
	}

	s.repos = make(map[string]*git.Repository, 0)
	for _, fixRepo := range fixtureRepos {
		s.repos[fixRepo.url] = git.NewPlainRepository()

		f, err := os.Open(fixRepo.packfile)
		c.Assert(err, IsNil)

		r := packfile.NewSeekable(f)
		d := packfile.NewDecoder(r)
		err = d.Decode(s.repos[fixRepo.url].Storage)
		c.Assert(err, IsNil)

		c.Assert(f.Close(), IsNil)
	}
}

func equalChanges(a, b Changes) bool {
	if a == nil && b == nil {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	if len(a) != len(b) {
		return false
	}

	sort.Sort(a)
	sort.Sort(b)

	for i, va := range a {
		vb := b[i]
		if va.Action != vb.Action ||
			va.File.Name != vb.File.Name ||
			va.File.Size != vb.File.Size {
			return false
		}
	}

	return true
}

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
	c.Assert(func() { action.String() },
		PanicMatches, "unsupported action: 37")
}

func (s *DiffTreeSuite) TestChangeString(c *C) {
	expected := "<Action: Insert, Path: bla, Size: 12>"
	change := &Change{
		Action: Insert,
		File:   &git.File{Name: "bla", Blob: git.Blob{Size: 12}},
	}
	obtained := change.String()
	c.Assert(obtained, Equals, expected)

	expected = "<Action: Delete, Path: bla, Size: 12>"
	change = &Change{
		Action: Delete,
		File:   &git.File{Name: "bla", Blob: git.Blob{Size: 12}},
	}
	obtained = change.String()
	c.Assert(obtained, Equals, expected)

	expected = "<Action: Modify, Path: bla, Size: 12>"
	change = &Change{
		Action: Modify,
		File:   &git.File{Name: "bla", Blob: git.Blob{Size: 12}},
	}
	obtained = change.String()
	c.Assert(obtained, Equals, expected)
}

func (s *DiffTreeSuite) TestChangesString(c *C) {
	expected := "[]"
	changes := newEmpty()
	obtained := changes.String()
	c.Assert(obtained, Equals, expected)

	expected = "[<Action: Modify, Path: bla, Size: 12>]"
	changes = make([]*Change, 1)
	changes[0] = &Change{Action: Modify, File: &git.File{Name: "bla", Blob: git.Blob{Size: 12}}}
	obtained = changes.String()
	c.Assert(obtained, Equals, expected)

	expected = "[<Action: Modify, Path: bla, Size: 12>, <Action: Insert, Path: foo/bar, Size: 124>]"
	changes = make([]*Change, 2)
	changes[0] = &Change{Action: Modify, File: &git.File{Name: "bla", Blob: git.Blob{Size: 12}}}
	changes[1] = &Change{Action: Insert, File: &git.File{Name: "foo/bar", Blob: git.Blob{Size: 124}}}
	obtained = changes.String()
	c.Assert(obtained, Equals, expected)
}

func (s *DiffTreeSuite) TestNew(c *C) {
	for i, t := range []struct {
		repo     string  // the repo name as in localRepos
		commit1  string  // the commit of the first tree
		commit2  string  // the commit of the second tree
		expected Changes // the expected list of changes
	}{
		{
			"git://github.com/dezfowler/LiteMock.git",
			"",
			"",
			Changes{},
		},
		{
			"git://github.com/dezfowler/LiteMock.git",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			Changes{},
		},
		{
			"git://github.com/dezfowler/LiteMock.git",
			"",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			Changes{
				{Action: Insert, File: &git.File{Name: "README", Blob: git.Blob{Size: 0}}},
			},
		},
		{
			"git://github.com/dezfowler/LiteMock.git",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			"",
			Changes{
				{Action: Delete, File: &git.File{Name: "README", Blob: git.Blob{Size: 0}}},
			},
		},
		{
			"git://github.com/githubtraining/example-branches.git",
			"",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			Changes{
				{Action: Insert, File: &git.File{Name: "README.md", Blob: git.Blob{Size: 359}}},
			},
		},
		{
			"git://github.com/githubtraining/example-branches.git",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			"",
			Changes{
				{Action: Delete, File: &git.File{Name: "README.md", Blob: git.Blob{Size: 359}}},
			},
		},
		{
			"git://github.com/githubtraining/example-branches.git",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			Changes{},
		},
		{
			"git://github.com/github/gem-builder.git",
			"",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			Changes{
				{Action: Insert, File: &git.File{Name: "README", Blob: git.Blob{Size: 1176}}},
				{Action: Insert, File: &git.File{Name: "gem_builder.rb", Blob: git.Blob{Size: 1956}}},
				{Action: Insert, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 687}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			"",
			Changes{
				{Action: Delete, File: &git.File{Name: "README", Blob: git.Blob{Size: 1176}}},
				{Action: Delete, File: &git.File{Name: "gem_builder.rb", Blob: git.Blob{Size: 1956}}},
				{Action: Delete, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 687}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			Changes{},
		},
		{
			"git://github.com/toqueteos/ts3.git",
			"",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			Changes{
				{Action: Insert, File: &git.File{Name: "examples/bot.go", Blob: git.Blob{Size: 287}}},
				{Action: Insert, File: &git.File{Name: "examples/raw_shell.go", Blob: git.Blob{Size: 1269}}},
				{Action: Insert, File: &git.File{Name: "helpers.go", Blob: git.Blob{Size: 782}}},
				{Action: Insert, File: &git.File{Name: "README.markdown", Blob: git.Blob{Size: 34}}},
				{Action: Insert, File: &git.File{Name: "ts3.go", Blob: git.Blob{Size: 4251}}},
			},
		},
		{
			"git://github.com/toqueteos/ts3.git",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			"",
			Changes{
				{Action: Delete, File: &git.File{Name: "examples/bot.go", Blob: git.Blob{Size: 287}}},
				{Action: Delete, File: &git.File{Name: "examples/raw_shell.go", Blob: git.Blob{Size: 1269}}},
				{Action: Delete, File: &git.File{Name: "helpers.go", Blob: git.Blob{Size: 782}}},
				{Action: Delete, File: &git.File{Name: "README.markdown", Blob: git.Blob{Size: 34}}},
				{Action: Delete, File: &git.File{Name: "ts3.go", Blob: git.Blob{Size: 4251}}},
			},
		},
		{
			"git://github.com/toqueteos/ts3.git",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			Changes{},
		},
		{
			"git://github.com/github/gem-builder.git",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			"6c41e05a17e19805879689414026eb4e279f7de0",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1828}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"6c41e05a17e19805879689414026eb4e279f7de0",
			"89be3aac2f178719c12953cc9eaa23441f8d9371",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1247}}},
				{Action: Insert, File: &git.File{Name: "gem_eval_test.rb", Blob: git.Blob{Size: 2245}}},
				{Action: Insert, File: &git.File{Name: "security.rb", Blob: git.Blob{Size: 1361}}},
				{Action: Insert, File: &git.File{Name: "security_test.rb", Blob: git.Blob{Size: 875}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"89be3aac2f178719c12953cc9eaa23441f8d9371",
			"597240b7da22d03ad555328f15abc480b820acc0",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1221}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"597240b7da22d03ad555328f15abc480b820acc0",
			"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1794}}},
				{Action: Modify, File: &git.File{Name: "gem_eval_test.rb", Blob: git.Blob{Size: 4220}}},
				{Action: Insert, File: &git.File{Name: "lazy_dir.rb", Blob: git.Blob{Size: 695}}},
				{Action: Insert, File: &git.File{Name: "lazy_dir_test.rb", Blob: git.Blob{Size: 1276}}},
				{Action: Modify, File: &git.File{Name: "security.rb", Blob: git.Blob{Size: 2134}}},
				{Action: Modify, File: &git.File{Name: "security_test.rb", Blob: git.Blob{Size: 1184}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
			"597240b7da22d03ad555328f15abc480b820acc0",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1221}}},
				{Action: Modify, File: &git.File{Name: "gem_eval_test.rb", Blob: git.Blob{Size: 2245}}},
				{Action: Delete, File: &git.File{Name: "lazy_dir.rb", Blob: git.Blob{Size: 695}}},
				{Action: Delete, File: &git.File{Name: "lazy_dir_test.rb", Blob: git.Blob{Size: 1276}}},
				{Action: Modify, File: &git.File{Name: "security.rb", Blob: git.Blob{Size: 1361}}},
				{Action: Modify, File: &git.File{Name: "security_test.rb", Blob: git.Blob{Size: 875}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
			"ca9fd470bacb6262eb4ca23ee48bb2f43711c1ff",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1768}}},
				{Action: Modify, File: &git.File{Name: "security.rb", Blob: git.Blob{Size: 2092}}},
				{Action: Modify, File: &git.File{Name: "security_test.rb", Blob: git.Blob{Size: 1180}}},
			},
		},
		{
			"git://github.com/github/gem-builder.git",
			"fe3c86745f887c23a0d38c85cfd87ca957312f86",
			"b7e3f636febf7a0cd3ab473b6d30081786d2c5b6",
			Changes{
				{Action: Modify, File: &git.File{Name: "gem_eval.rb", Blob: git.Blob{Size: 1696}}},
				{Action: Modify, File: &git.File{Name: "gem_eval_test.rb", Blob: git.Blob{Size: 4622}}},
				{Action: Insert, File: &git.File{Name: "git_mock", Blob: git.Blob{Size: 45}}},
				{Action: Modify, File: &git.File{Name: "lazy_dir.rb", Blob: git.Blob{Size: 921}}},
				{Action: Modify, File: &git.File{Name: "lazy_dir_test.rb", Blob: git.Blob{Size: 1617}}},
				{Action: Modify, File: &git.File{Name: "security.rb", Blob: git.Blob{Size: 2179}}},
			},
		},
		{
			"git://github.com/rumpkernel/rumprun-xen.git",
			"1831e47b0c6db750714cd0e4be97b5af17fb1eb0",
			"51d8515578ea0c88cc8fc1a057903675cf1fc16c",
			Changes{
				{Action: Modify, File: &git.File{Name: "Makefile", Blob: git.Blob{Size: 4166}}},
				{Action: Modify, File: &git.File{Name: "netbsd_init.c", Blob: git.Blob{Size: 953}}},
				{Action: Modify, File: &git.File{Name: "rumphyper_stubs.c", Blob: git.Blob{Size: 766}}},
				{Action: Delete, File: &git.File{Name: "sysproxy.c", Blob: git.Blob{Size: 47101}}},
			},
		},
		{
			"git://github.com/rumpkernel/rumprun-xen.git",
			"1831e47b0c6db750714cd0e4be97b5af17fb1eb0",
			"e13e678f7ee9badd01b120889e0ec5fdc8ae3802",
			Changes{
				{Action: Modify, File: &git.File{Name: "app-tools/rumprun", Blob: git.Blob{Size: 6492}}},
			},
		},
	} {
		repo, ok := s.repos[t.repo]
		c.Assert(ok, Equals, true,
			Commentf("subtest %d: repo %s not found", i, t.repo))

		tree1, err := tree(repo, t.commit1)
		c.Assert(err, IsNil,
			Commentf("subtest %d: unable to retrieve tree from commit %s and repo %s: %s", i, t.commit1, t.repo, err))

		var tree2 *git.Tree
		if t.commit1 == t.commit2 {
			tree2 = tree1
		} else {
			tree2, err = tree(repo, t.commit2)
			c.Assert(err, IsNil,
				Commentf("subtest %d: unable to retrieve tree from commit %s and repo %s", i, t.commit2, t.repo, err))
		}

		obtained, err := New(tree1, tree2)
		c.Assert(err, IsNil,
			Commentf("subtest %d: unable to calculate difftree: %s", i, err))
		c.Assert(equalChanges(obtained, t.expected), Equals, true,
			Commentf("subtest:%d\nrepo=%s\ncommit1=%s\ncommit2=%s\nexpected=%s\nobtained=%s",
				i, t.repo, t.commit1, t.commit2, t.expected, obtained))
	}
}

func tree(repo *git.Repository, commitHashStr string) (*git.Tree, error) {
	if commitHashStr == "" {
		return nil, nil
	}

	commit, err := repo.Commit(core.NewHash(commitHashStr))
	if err != nil {
		return nil, err
	}

	return commit.Tree(), nil
}
