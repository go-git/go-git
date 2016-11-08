package git

import (
	"fmt"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

type TagSuite struct {
	BaseSuite
}

var _ = Suite(&TagSuite{})

func (s *TagSuite) SetUpSuite(c *C) {
	s.BaseSuite.SetUpSuite(c)
	s.buildRepositories(c, fixtures.ByTag("tags"))
}

func (s *TagSuite) TestName(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(err, IsNil)
	c.Assert(tag.Name, Equals, "annotated-tag")
}

func (s *TagSuite) TestTagger(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(err, IsNil)
	c.Assert(tag.Tagger.String(), Equals, "Máximo Cuadros <mcuadros@gmail.com>")
}

func (s *TagSuite) TestAnnotated(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(err, IsNil)
	c.Assert(tag.Message, Equals, "example annotated tag\n")

	commit, err := tag.Commit()
	c.Assert(err, IsNil)
	c.Assert(commit.Type(), Equals, plumbing.CommitObject)
	c.Assert(commit.ID().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
}

func (s *TagSuite) TestCommit(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	c.Assert(err, IsNil)
	c.Assert(tag.Message, Equals, "a tagged commit\n")

	commit, err := tag.Commit()
	c.Assert(err, IsNil)
	c.Assert(commit.Type(), Equals, plumbing.CommitObject)
	c.Assert(commit.ID().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
}

func (s *TagSuite) TestBlob(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))
	c.Assert(err, IsNil)
	c.Assert(tag.Message, Equals, "a tagged blob\n")

	blob, err := tag.Blob()
	c.Assert(err, IsNil)
	c.Assert(blob.Type(), Equals, plumbing.BlobObject)
	c.Assert(blob.ID().String(), Equals, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
}

func (s *TagSuite) TestTree(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("152175bf7e5580299fa1f0ba41ef6474cc043b70"))
	c.Assert(err, IsNil)
	c.Assert(tag.Message, Equals, "a tagged tree\n")

	tree, err := tag.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.Type(), Equals, plumbing.TreeObject)
	c.Assert(tree.ID().String(), Equals, "70846e9a10ef7b41064b40f07713d5b8b9a8fc73")
}

func (s *TagSuite) TestTreeFromCommit(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	c.Assert(err, IsNil)
	c.Assert(tag.Message, Equals, "a tagged commit\n")

	tree, err := tag.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.Type(), Equals, plumbing.TreeObject)
	c.Assert(tree.ID().String(), Equals, "70846e9a10ef7b41064b40f07713d5b8b9a8fc73")
}

func (s *TagSuite) TestObject(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	c.Assert(err, IsNil)

	obj, err := tag.Object()
	c.Assert(err, IsNil)
	c.Assert(obj.Type(), Equals, plumbing.CommitObject)
	c.Assert(obj.ID().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
}

func (s *TagSuite) TestTagItter(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]
	iter, err := r.s.IterObjects(plumbing.TagObject)
	c.Assert(err, IsNil)

	var count int
	i := NewTagIter(r, iter)
	err = i.ForEach(func(t *Tag) error {
		count++
		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(count, Equals, 4)
}

func (s *TagSuite) TestTagIterError(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]
	iter, err := r.s.IterObjects(plumbing.TagObject)
	c.Assert(err, IsNil)

	i := NewTagIter(r, iter)
	err = i.ForEach(func(t *Tag) error {
		return fmt.Errorf("a random error")
	})

	c.Assert(err, NotNil)
}

func (s *TagSuite) TestTagEncodeDecodeIdempotent(c *C) {
	ts, err := time.Parse(time.RFC3339, "2006-01-02T15:04:05-07:00")
	c.Assert(err, IsNil)
	tags := []*Tag{
		{
			Name:       "foo",
			Tagger:     Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Message:    "Message\n\nFoo\nBar\nBaz\n\n",
			TargetType: plumbing.BlobObject,
			Target:     plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		},
		{
			Name:       "foo",
			Tagger:     Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			TargetType: plumbing.BlobObject,
			Target:     plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		},
	}
	for _, tag := range tags {
		obj := &plumbing.MemoryObject{}
		err = tag.Encode(obj)
		c.Assert(err, IsNil)
		newTag := &Tag{}
		err = newTag.Decode(obj)
		c.Assert(err, IsNil)
		tag.Hash = obj.Hash()
		c.Assert(newTag, DeepEquals, tag)
	}
}

func (s *TagSuite) TestString(c *C) {
	r := s.Repositories["https://github.com/git-fixtures/tags.git"]

	tag, err := r.Tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(err, IsNil)
	c.Assert(tag.String(), Equals, ""+
		"tag annotated-tag\n"+
		"Tagger: Máximo Cuadros <mcuadros@gmail.com>\n"+
		"Date:   Wed Sep 21 21:13:35 2016 +0200\n"+
		"\n"+
		"example annotated tag\n"+
		"\n"+
		"commit f7b877701fbf855b44c0a9e86f3fdce2c298b07f\n"+
		"Author: Máximo Cuadros <mcuadros@gmail.com>\n"+
		"Date:   Wed Sep 21 21:10:52 2016 +0200\n"+
		"\n"+
		"    initial\n"+
		"\n",
	)
}
