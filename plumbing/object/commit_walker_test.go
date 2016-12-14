package object

import . "gopkg.in/check.v1"

type CommitWalkerSuite struct {
	BaseObjectsSuite
}

var _ = Suite(&CommitWalkerSuite{})

func (s *CommitWalkerSuite) TestWalkerNext(c *C) {
	commit := s.commit(c, s.Fixture.Head)

	var commits []*Commit

	WalkCommitHistory(commit, func(c *Commit) error {
		commits = append(commits, c)
		return nil
	})

	SortCommits(commits)
	c.Assert(commits, HasLen, 8)

	expected := []string{
		"b029517f6300c2da0f4b651b8642506cd6aaf45d", "b8e471f58bcbca63b07bda20e428190409c2db47",
		"35e85108805c84807bc66a02d91535e1e24b38b9", "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"1669dce138d9b841a518c64b10914d88f5e488ea", "af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
		"918c48b83bd081e863dbe1b80f8998f058cd8294", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	}

	for i, commit := range commits {
		c.Assert(commit.Hash.String(), Equals, expected[i])
	}
}
