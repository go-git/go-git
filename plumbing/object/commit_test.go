package object

import (
	"io"
	"time"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

type SuiteCommit struct {
	BaseObjectsSuite
	Commit *Commit
}

var _ = Suite(&SuiteCommit{})

func (s *SuiteCommit) SetUpSuite(c *C) {
	s.BaseObjectsSuite.SetUpSuite(c)

	hash := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")

	s.Commit = s.commit(c, hash)
}

func (s *SuiteCommit) TestDecodeNonCommit(c *C) {
	hash := plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")
	blob, err := s.Storer.EncodedObject(plumbing.AnyObject, hash)
	c.Assert(err, IsNil)

	commit := &Commit{}
	err = commit.Decode(blob)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *SuiteCommit) TestType(c *C) {
	c.Assert(s.Commit.Type(), Equals, plumbing.CommitObject)
}

func (s *SuiteCommit) TestTree(c *C) {
	tree, err := s.Commit.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.ID().String(), Equals, "eba74343e2f15d62adedfd8c883ee0262b5c8021")
}

func (s *SuiteCommit) TestParents(c *C) {
	expected := []string{
		"35e85108805c84807bc66a02d91535e1e24b38b9",
		"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
	}

	var output []string
	i := s.Commit.Parents()
	err := i.ForEach(func(commit *Commit) error {
		output = append(output, commit.ID().String())
		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, expected)
}

func (s *SuiteCommit) TestCommitEncodeDecodeIdempotent(c *C) {
	ts, err := time.Parse(time.RFC3339, "2006-01-02T15:04:05-07:00")
	c.Assert(err, IsNil)
	commits := []*Commit{
		{
			Author:    Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer: Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:   "Message\n\nFoo\nBar\nWith trailing blank lines\n\n",
			tree:      plumbing.NewHash("f000000000000000000000000000000000000001"),
			parents:   []plumbing.Hash{plumbing.NewHash("f000000000000000000000000000000000000002")},
		},
		{
			Author:    Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer: Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:   "Message\n\nFoo\nBar\nWith no trailing blank lines",
			tree:      plumbing.NewHash("0000000000000000000000000000000000000003"),
			parents: []plumbing.Hash{
				plumbing.NewHash("f000000000000000000000000000000000000004"),
				plumbing.NewHash("f000000000000000000000000000000000000005"),
				plumbing.NewHash("f000000000000000000000000000000000000006"),
				plumbing.NewHash("f000000000000000000000000000000000000007"),
			},
		},
	}
	for _, commit := range commits {
		obj := &plumbing.MemoryObject{}
		err = commit.Encode(obj)
		c.Assert(err, IsNil)
		newCommit := &Commit{}
		err = newCommit.Decode(obj)
		c.Assert(err, IsNil)
		commit.Hash = obj.Hash()
		c.Assert(newCommit, DeepEquals, commit)
	}
}

func (s *SuiteCommit) TestFile(c *C) {
	file, err := s.Commit.File("CHANGELOG")
	c.Assert(err, IsNil)
	c.Assert(file.Name, Equals, "CHANGELOG")
}

func (s *SuiteCommit) TestNumParents(c *C) {
	c.Assert(s.Commit.NumParents(), Equals, 2)
}

func (s *SuiteCommit) TestHistory(c *C) {
	commits, err := s.Commit.History()
	c.Assert(err, IsNil)
	c.Assert(commits, HasLen, 5)
	c.Assert(commits[0].Hash.String(), Equals, s.Commit.Hash.String())
	c.Assert(commits[len(commits)-1].Hash.String(), Equals, "b029517f6300c2da0f4b651b8642506cd6aaf45d")
}

func (s *SuiteCommit) TestString(c *C) {
	c.Assert(s.Commit.String(), Equals, ""+
		"commit 1669dce138d9b841a518c64b10914d88f5e488ea\n"+
		"Author: Máximo Cuadros Ortiz <mcuadros@gmail.com>\n"+
		"Date:   Tue Mar 31 13:48:14 2015 +0200\n"+
		"\n"+
		"    Merge branch 'master' of github.com:tyba/git-fixture\n"+
		"\n",
	)
}

func (s *SuiteCommit) TestStringMultiLine(c *C) {
	hash := plumbing.NewHash("e7d896db87294e33ca3202e536d4d9bb16023db3")

	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()
	sto, err := filesystem.NewStorage(f.DotGit())

	o, err := sto.EncodedObject(plumbing.CommitObject, hash)
	c.Assert(err, IsNil)
	commit, err := DecodeCommit(sto, o)
	c.Assert(err, IsNil)

	c.Assert(commit.String(), Equals, ""+
		"commit e7d896db87294e33ca3202e536d4d9bb16023db3\n"+
		"Author: Alberto Cortés <alberto@sourced.tech>\n"+
		"Date:   Wed Jan 27 11:13:49 2016 +0100\n"+
		"\n"+
		"    fix zlib invalid header error\n"+
		"\n"+
		"    The return value of reads to the packfile were being ignored, so zlib\n"+
		"    was getting invalid data on it read buffers.\n"+
		"\n",
	)
}

func (s *SuiteCommit) TestCommitIterNext(c *C) {
	i := s.Commit.Parents()

	commit, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(commit.ID().String(), Equals, "35e85108805c84807bc66a02d91535e1e24b38b9")

	commit, err = i.Next()
	c.Assert(err, IsNil)
	c.Assert(commit.ID().String(), Equals, "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69")

	commit, err = i.Next()
	c.Assert(err, Equals, io.EOF)
	c.Assert(commit, IsNil)
}
