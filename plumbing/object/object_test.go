package object

import (
	"io"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type BaseObjectsFixtureSuite struct{}

type BaseObjectsSuite struct {
	Storer  storer.EncodedObjectStorer
	Fixture *fixtures.Fixture
	t       *testing.T
}

func (s *BaseObjectsSuite) SetupSuite(t *testing.T) {
	s.Fixture = fixtures.Basic().One()
	storer := filesystem.NewStorage(s.Fixture.DotGit(), cache.NewObjectLRUDefault())
	s.Storer = storer
	s.t = t
}

func (s *BaseObjectsSuite) tag(h plumbing.Hash) *Tag {
	t, err := GetTag(s.Storer, h)
	assert.NoError(s.t, err)
	return t
}

func (s *BaseObjectsSuite) tree(h plumbing.Hash) *Tree {
	t, err := GetTree(s.Storer, h)
	assert.NoError(s.t, err)
	return t
}

func (s *BaseObjectsSuite) commit(h plumbing.Hash) *Commit {
	commit, err := GetCommit(s.Storer, h)
	assert.NoError(s.t, err)
	return commit
}

type ObjectsSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func TestObjectsSuite(t *testing.T) {
	suite.Run(t, new(ObjectsSuite))
}

func (s *ObjectsSuite) SetupSuite() {
	s.BaseObjectsSuite.SetupSuite(s.T())
}

func (s *ObjectsSuite) TestNewCommit() {
	hash := plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69")
	commit := s.commit(hash)

	s.Equal(commit.ID(), commit.Hash)
	s.Equal("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69", commit.Hash.String())

	tree, err := commit.Tree()
	s.NoError(err)
	s.Equal("c2d30fa8ef288618f65f6eed6e168e0d514886f4", tree.Hash.String())

	parents := commit.Parents()
	parentCommit, err := parents.Next()
	s.NoError(err)
	s.Equal("b029517f6300c2da0f4b651b8642506cd6aaf45d", parentCommit.Hash.String())

	parentCommit, err = parents.Next()
	s.NoError(err)
	s.Equal("b8e471f58bcbca63b07bda20e428190409c2db47", parentCommit.Hash.String())

	s.Equal("mcuadros@gmail.com", commit.Author.Email)
	s.Equal("Máximo Cuadros", commit.Author.Name)
	s.Equal("2015-03-31T13:47:14+02:00", commit.Author.When.Format(time.RFC3339))
	s.Equal("mcuadros@gmail.com", commit.Committer.Email)
	s.Equal("Merge pull request #1 from dripolles/feature\n\nCreating changelog", commit.Message)
}

func (s *ObjectsSuite) TestParseTree() {
	hash := plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c")
	tree, err := GetTree(s.Storer, hash)
	s.NoError(err)

	s.Len(tree.Entries, 8)

	tree.buildMap()
	s.Len(tree.m, 8)
	s.Equal(".gitignore", tree.m[".gitignore"].Name)
	s.Equal(filemode.Regular, tree.m[".gitignore"].Mode)
	s.Equal("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", tree.m[".gitignore"].Hash.String())

	count := 0
	iter := tree.Files()
	defer iter.Close()
	for f, err := iter.Next(); err == nil; f, err = iter.Next() {
		count++
		if f.Name == "go/example.go" {
			reader, err := f.Reader()
			s.NoError(err)
			defer func() { s.Nil(reader.Close()) }()
			content, _ := io.ReadAll(reader)
			s.Len(content, 2780)
		}
	}

	s.Equal(9, count)
}

func (s *ObjectsSuite) TestParseSignature() {
	cases := map[string]Signature{
		`Foo Bar <foo@bar.com> 1257894000 +0100`: {
			Name:  "Foo Bar",
			Email: "foo@bar.com",
			When:  MustParseTime("2009-11-11 00:00:00 +0100"),
		},
		`Foo Bar <foo@bar.com> 1257894000 -0700`: {
			Name:  "Foo Bar",
			Email: "foo@bar.com",
			When:  MustParseTime("2009-11-10 16:00:00 -0700"),
		},
		`Foo Bar <> 1257894000 +0100`: {
			Name:  "Foo Bar",
			Email: "",
			When:  MustParseTime("2009-11-11 00:00:00 +0100"),
		},
		` <> 1257894000`: {
			Name:  "",
			Email: "",
			When:  MustParseTime("2009-11-10 23:00:00 +0000"),
		},
		`Foo Bar <foo@bar.com>`: {
			Name:  "Foo Bar",
			Email: "foo@bar.com",
			When:  time.Time{},
		},
		`crap> <foo@bar.com> 1257894000 +1000`: {
			Name:  "crap>",
			Email: "foo@bar.com",
			When:  MustParseTime("2009-11-11 09:00:00 +1000"),
		},
		`><`: {
			Name:  "",
			Email: "",
			When:  time.Time{},
		},
		``: {
			Name:  "",
			Email: "",
			When:  time.Time{},
		},
		`<`: {
			Name:  "",
			Email: "",
			When:  time.Time{},
		},
	}

	for raw, exp := range cases {
		got := &Signature{}
		got.Decode([]byte(raw))

		s.Equal(exp.Name, got.Name)
		s.Equal(exp.Email, got.Email)
		s.Equal(exp.When.Format(time.RFC3339), got.When.Format(time.RFC3339))
	}
}

func (s *ObjectsSuite) TestObjectIter() {
	encIter, err := s.Storer.IterEncodedObjects(plumbing.AnyObject)
	s.NoError(err)
	iter := NewObjectIter(s.Storer, encIter)

	objects := []Object{}
	iter.ForEach(func(o Object) error {
		objects = append(objects, o)
		return nil
	})

	s.True(len(objects) > 0)
	iter.Close()

	encIter, err = s.Storer.IterEncodedObjects(plumbing.AnyObject)
	s.NoError(err)
	iter = NewObjectIter(s.Storer, encIter)

	i := 0
	for {
		o, err := iter.Next()
		if err == io.EOF {
			break
		}

		s.NoError(err)
		s.Equal(objects[i].ID(), o.ID())
		s.Equal(objects[i].Type(), o.Type())
		i++
	}

	iter.Close()
}

func MustParseTime(value string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04:05 -0700", value)
	return t
}
