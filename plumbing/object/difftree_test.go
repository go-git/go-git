package object

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

type DiffTreeSuite struct {
	suite.Suite
	Storer  storer.EncodedObjectStorer
	Fixture *fixtures.Fixture
	cache   map[string]storer.EncodedObjectStorer
}

func (s *DiffTreeSuite) SetupSuite() {
	s.Fixture = fixtures.Basic().One()
	dotgit, err := s.Fixture.DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	s.T().Cleanup(func() {
		_ = sto.Close()
	})
	s.Storer = sto
	s.cache = make(map[string]storer.EncodedObjectStorer)
}

func (s *DiffTreeSuite) commitFromStorer(sto storer.EncodedObjectStorer,
	h plumbing.Hash,
) *Commit {
	commit, err := GetCommit(s.T().Context(), sto, h)
	s.NoError(err)
	return commit
}

func (s *DiffTreeSuite) storageFromPackfile(f *fixtures.Fixture) storer.EncodedObjectStorer {
	sto, ok := s.cache[f.URL]
	if ok {
		return sto
	}

	storer := memory.NewStorage()

	pf, err := f.Packfile()
	if err != nil {
		panic(err)
	}
	defer pf.Close()

	if err := packfile.UpdateObjectStorage(s.T().Context(), storer, pf); err != nil {
		panic(err)
	}

	s.cache[f.URL] = storer
	return storer
}

func TestDiffTreeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(DiffTreeSuite))
}

type expectChange struct {
	Action merkletrie.Action
	Name   string
}

func assertChanges(a Changes, s *DiffTreeSuite) {
	for _, changes := range a {
		action, err := changes.Action()
		s.NoError(err)
		switch action {
		case merkletrie.Insert:
			s.Nil(changes.From.Tree)
			s.NotNil(changes.To.Tree)
		case merkletrie.Delete:
			s.NotNil(changes.From.Tree)
			s.Nil(changes.To.Tree)
		case merkletrie.Modify:
			s.NotNil(changes.From.Tree)
			s.NotNil(changes.To.Tree)
		default:
			s.Fail("unknown action:", action)
		}
	}
}

func equalChanges(a Changes, b []expectChange, s *DiffTreeSuite) bool {
	if len(a) != len(b) {
		return false
	}

	sort.Sort(a)

	for i, va := range a {
		vb := b[i]
		action, err := va.Action()
		s.NoError(err)
		if action != vb.Action || va.name() != vb.Name {
			return false
		}
	}

	return true
}

func (s *DiffTreeSuite) TestDiffTree() {
	for i, t := range []struct {
		repository string         // the repo name as in localRepos
		commit1    string         // the commit of the first tree
		commit2    string         // the commit of the second tree
		expected   []expectChange // the expected list of []changeExpect
	}{
		{
			"https://github.com/dezfowler/LiteMock.git",
			"",
			"",
			[]expectChange{},
		},
		{
			"https://github.com/dezfowler/LiteMock.git",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			[]expectChange{},
		},
		{
			"https://github.com/dezfowler/LiteMock.git",
			"",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			[]expectChange{
				{Action: merkletrie.Insert, Name: "README"},
			},
		},
		{
			"https://github.com/dezfowler/LiteMock.git",
			"b7965eaa2c4f245d07191fe0bcfe86da032d672a",
			"",
			[]expectChange{
				{Action: merkletrie.Delete, Name: "README"},
			},
		},
		{
			"https://github.com/githubtraining/example-branches.git",
			"",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			[]expectChange{
				{Action: merkletrie.Insert, Name: "README.md"},
			},
		},
		{
			"https://github.com/githubtraining/example-branches.git",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			"",
			[]expectChange{
				{Action: merkletrie.Delete, Name: "README.md"},
			},
		},
		{
			"https://github.com/githubtraining/example-branches.git",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			"f0eb272cc8f77803478c6748103a1450aa1abd37",
			[]expectChange{},
		},
		{
			"https://github.com/github/gem-builder.git",
			"",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			[]expectChange{
				{Action: merkletrie.Insert, Name: "README"},
				{Action: merkletrie.Insert, Name: "gem_builder.rb"},
				{Action: merkletrie.Insert, Name: "gem_eval.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			"",
			[]expectChange{
				{Action: merkletrie.Delete, Name: "README"},
				{Action: merkletrie.Delete, Name: "gem_builder.rb"},
				{Action: merkletrie.Delete, Name: "gem_eval.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			[]expectChange{},
		},
		{
			"https://github.com/toqueteos/ts3.git",
			"",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			[]expectChange{
				{Action: merkletrie.Insert, Name: "README.markdown"},
				{Action: merkletrie.Insert, Name: "examples/bot.go"},
				{Action: merkletrie.Insert, Name: "examples/raw_shell.go"},
				{Action: merkletrie.Insert, Name: "helpers.go"},
				{Action: merkletrie.Insert, Name: "ts3.go"},
			},
		},
		{
			"https://github.com/toqueteos/ts3.git",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			"",
			[]expectChange{
				{Action: merkletrie.Delete, Name: "README.markdown"},
				{Action: merkletrie.Delete, Name: "examples/bot.go"},
				{Action: merkletrie.Delete, Name: "examples/raw_shell.go"},
				{Action: merkletrie.Delete, Name: "helpers.go"},
				{Action: merkletrie.Delete, Name: "ts3.go"},
			},
		},
		{
			"https://github.com/toqueteos/ts3.git",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			"764e914b75d6d6df1fc5d832aa9840f590abf1bb",
			[]expectChange{},
		},
		{
			"https://github.com/github/gem-builder.git",
			"9608eed92b3839b06ebf72d5043da547de10ce85",
			"6c41e05a17e19805879689414026eb4e279f7de0",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"6c41e05a17e19805879689414026eb4e279f7de0",
			"89be3aac2f178719c12953cc9eaa23441f8d9371",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
				{Action: merkletrie.Insert, Name: "gem_eval_test.rb"},
				{Action: merkletrie.Insert, Name: "security.rb"},
				{Action: merkletrie.Insert, Name: "security_test.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"89be3aac2f178719c12953cc9eaa23441f8d9371",
			"597240b7da22d03ad555328f15abc480b820acc0",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"597240b7da22d03ad555328f15abc480b820acc0",
			"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
				{Action: merkletrie.Modify, Name: "gem_eval_test.rb"},
				{Action: merkletrie.Insert, Name: "lazy_dir.rb"},
				{Action: merkletrie.Insert, Name: "lazy_dir_test.rb"},
				{Action: merkletrie.Modify, Name: "security.rb"},
				{Action: merkletrie.Modify, Name: "security_test.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
			"597240b7da22d03ad555328f15abc480b820acc0",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
				{Action: merkletrie.Modify, Name: "gem_eval_test.rb"},
				{Action: merkletrie.Delete, Name: "lazy_dir.rb"},
				{Action: merkletrie.Delete, Name: "lazy_dir_test.rb"},
				{Action: merkletrie.Modify, Name: "security.rb"},
				{Action: merkletrie.Modify, Name: "security_test.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"0260380e375d2dd0e1a8fcab15f91ce56dbe778e",
			"ca9fd470bacb6262eb4ca23ee48bb2f43711c1ff",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
				{Action: merkletrie.Modify, Name: "security.rb"},
				{Action: merkletrie.Modify, Name: "security_test.rb"},
			},
		},
		{
			"https://github.com/github/gem-builder.git",
			"fe3c86745f887c23a0d38c85cfd87ca957312f86",
			"b7e3f636febf7a0cd3ab473b6d30081786d2c5b6",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "gem_eval.rb"},
				{Action: merkletrie.Modify, Name: "gem_eval_test.rb"},
				{Action: merkletrie.Insert, Name: "git_mock"},
				{Action: merkletrie.Modify, Name: "lazy_dir.rb"},
				{Action: merkletrie.Modify, Name: "lazy_dir_test.rb"},
				{Action: merkletrie.Modify, Name: "security.rb"},
			},
		},
		{
			"https://github.com/rumpkernel/rumprun-xen.git",
			"1831e47b0c6db750714cd0e4be97b5af17fb1eb0",
			"51d8515578ea0c88cc8fc1a057903675cf1fc16c",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "Makefile"},
				{Action: merkletrie.Modify, Name: "netbsd_init.c"},
				{Action: merkletrie.Modify, Name: "rumphyper_stubs.c"},
				{Action: merkletrie.Delete, Name: "sysproxy.c"},
			},
		},
		{
			"https://github.com/rumpkernel/rumprun-xen.git",
			"1831e47b0c6db750714cd0e4be97b5af17fb1eb0",
			"e13e678f7ee9badd01b120889e0ec5fdc8ae3802",
			[]expectChange{
				{Action: merkletrie.Modify, Name: "app-tools/rumprun"},
			},
		},
	} {
		f := fixtures.ByURL(t.repository).One()
		sto := s.storageFromPackfile(f)

		var tree1, tree2 *Tree
		var err error
		if t.commit1 != "" {
			tree1, err = s.commitFromStorer(sto,
				plumbing.NewHash(t.commit1)).Tree(s.T().Context())
			s.NoError(err,
				fmt.Sprintf("subtest %d: unable to retrieve tree from commit %s and repo %s: %s", i, t.commit1, t.repository, err))
		}

		if t.commit2 != "" {
			tree2, err = s.commitFromStorer(sto,
				plumbing.NewHash(t.commit2)).Tree(s.T().Context())
			s.NoError(err,
				fmt.Sprintf("subtest %d: unable to retrieve tree from commit %s and repo %s", i, t.commit2, t.repository))
		}

		obtained, err := DiffTree(s.T().Context(), tree1, tree2)
		s.NoError(err,
			fmt.Sprintf("subtest %d: unable to calculate difftree: %s", i, err))
		obtainedFromMethod, err := tree1.Diff(s.T().Context(), tree2)
		s.NoError(err,
			fmt.Sprintf("subtest %d: unable to calculate difftree: %s. Result calling Diff method from Tree object returns an error", i, err))

		s.Equal(obtainedFromMethod, obtained)

		s.True(equalChanges(obtained, t.expected, s),
			fmt.Sprintf("subtest:%d\nrepo=%s\ncommit1=%s\ncommit2=%s\nexpected=%s\nobtained=%s",
				i, t.repository, t.commit1, t.commit2, t.expected, obtained))

		assertChanges(obtained, s)
	}
}

func (s *DiffTreeSuite) TestIssue279() {
	// treeNoders should have the same hash when their mode is
	// filemode.Deprecated and filemode.Regular.
	a := &treeNoder{
		hash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		mode: filemode.Regular,
	}
	b := &treeNoder{
		hash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		mode: filemode.Deprecated,
	}
	s.Equal(b.Hash(), a.Hash())

	// yet, they should have different hashes if their contents change.
	aa := &treeNoder{
		hash: plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		mode: filemode.Regular,
	}
	s.NotEqual(aa.Hash(), a.Hash())
	bb := &treeNoder{
		hash: plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		mode: filemode.Deprecated,
	}
	s.NotEqual(bb.Hash(), b.Hash())
}

// DiffTree computes a diff in memory and never materialises entry names into a
// worktree, so it must not reject trees whose entry names contain bytes that
// upstream Git's verify_path accepts — a control character such as a newline is
// a valid pathname byte on disk (only '/' and NUL are forbidden in a tree entry
// name). The path-safety validation in TreeWalker.Next is for callers that hand
// names to the filesystem; the diff walk opts out of it.
func TestDiffTreeAllowsControlCharInPath(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	blob := storeTestObject(t, st, plumbing.BlobObject, []byte("IO.puts(\"hi\")\n"))

	const ctrlName = "foo\n.exs"
	empty := storeTestTree(t, st, nil)
	withFile := storeTestTree(t, st, []TreeEntry{
		{Name: ctrlName, Mode: filemode.Regular, Hash: blob},
	})

	from, err := GetTree(t.Context(), st, empty)
	if err != nil {
		t.Fatalf("GetTree(empty) = %v", err)
	}
	to, err := GetTree(t.Context(), st, withFile)
	if err != nil {
		t.Fatalf("GetTree(withFile) = %v", err)
	}

	changes, err := DiffTree(t.Context(), from, to)
	if err != nil {
		t.Fatalf("DiffTree on a tree with a control-char path = %v; want nil", err)
	}
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if got := changes[0].To.Name; got != ctrlName {
		t.Fatalf("change name = %q, want %q", got, ctrlName)
	}
}

// The diff walk opting out of path validation must not weaken the default
// TreeWalker, which materialising callers (archive, FileIter) rely on to reject
// names that are unsafe to write to disk.
func TestTreeWalkerValidatesControlCharByDefault(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	blob := storeTestObject(t, st, plumbing.BlobObject, []byte("x\n"))
	withFile := storeTestTree(t, st, []TreeEntry{
		{Name: "foo\n.exs", Mode: filemode.Regular, Hash: blob},
	})
	tree, err := GetTree(t.Context(), st, withFile)
	if err != nil {
		t.Fatalf("GetTree = %v", err)
	}

	w := NewTreeWalker(tree, true, nil)
	defer w.Close()
	_, _, err = w.Next(t.Context())
	if !errors.Is(err, pathutil.ErrInvalidPath) {
		t.Fatalf("TreeWalker.Next error = %v, want pathutil.ErrInvalidPath", err)
	}
}

func storeTestObject(t *testing.T, st storer.EncodedObjectStorer, typ plumbing.ObjectType, content []byte) plumbing.Hash {
	t.Helper()
	o := st.NewEncodedObject()
	o.SetType(typ)
	w, err := o.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := st.SetEncodedObject(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// storeTestTree writes a raw tree object, bypassing Tree.Encode's Validate gate
// so a tree with an otherwise-rejected entry name can be created — mirroring how
// such a tree arrives off the wire and is read back via the permissive Decode.
func storeTestTree(t *testing.T, st storer.EncodedObjectStorer, entries []TreeEntry) plumbing.Hash {
	t.Helper()
	o := st.NewEncodedObject()
	o.SetType(plumbing.TreeObject)
	w, err := o.Writer()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(w, "%o %s", e.Mode, e.Name); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte{0x00}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Hash.WriteTo(w); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := st.SetEncodedObject(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
