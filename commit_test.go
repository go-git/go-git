package git

import (
	"io"

	"gopkg.in/src-d/go-git.v4/core"

	. "gopkg.in/check.v1"
)

type SuiteCommit struct {
	BaseSuite
	Commit *Commit
}

var _ = Suite(&SuiteCommit{})

func (s *SuiteCommit) SetUpTest(c *C) {
	s.BaseSuite.SetUpTest(c)

	hash := core.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")

	var err error
	s.Commit, err = s.Repository.Commit(hash)
	c.Assert(err, IsNil)
}

func (s *SuiteCommit) TestDecodeNonCommit(c *C) {
	hash := core.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")
	blob, err := s.Repository.s.ObjectStorage().Get(hash)
	c.Assert(err, IsNil)

	commit := &Commit{}
	err = commit.Decode(blob)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *SuiteCommit) TestType(c *C) {
	c.Assert(s.Commit.Type(), Equals, core.CommitObject)
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

func (s *SuiteCommit) TestFile(c *C) {
	file, err := s.Commit.File("CHANGELOG")
	c.Assert(err, IsNil)
	c.Assert(file.Name, Equals, "CHANGELOG")
}

func (s *SuiteCommit) TestNumParents(c *C) {
	c.Assert(s.Commit.NumParents(), Equals, 2)
}

func (s *SuiteCommit) TestString(c *C) {
	c.Assert(s.Commit.String(), Equals, ""+
		"commit 1669dce138d9b841a518c64b10914d88f5e488ea\n"+
		"Author: Máximo Cuadros Ortiz <mcuadros@gmail.com>\n"+
		"Date:   2015-03-31 13:48:14 +0200 +0200\n",
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
