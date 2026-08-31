package config

import (
	"github.com/go-git/go-git/v5/plumbing"

	. "gopkg.in/check.v1"
)

type BranchSuite struct{}

var _ = Suite(&BranchSuite{})

func (b *BranchSuite) TestValidateName(c *C) {
	goodBranch := Branch{
		Name:   "master",
		Remote: "some_remote",
		Merge:  "refs/heads/master",
	}
	badBranch := Branch{
		Remote: "some_remote",
		Merge:  "refs/heads/master",
	}
	c.Assert(goodBranch.Validate(), IsNil)
	c.Assert(badBranch.Validate(), NotNil)
}

func (b *BranchSuite) TestValidateMerge(c *C) {
	goodBranch := Branch{
		Name:   "master",
		Remote: "some_remote",
		Merge:  "refs/heads/master",
	}
	badBranch := Branch{
		Name:   "master",
		Remote: "some_remote",
		Merge:  "blah",
	}
	c.Assert(goodBranch.Validate(), IsNil)
	c.Assert(badBranch.Validate(), NotNil)
}

func (b *BranchSuite) TestMarshal(c *C) {
	expected := []byte(`[core]
	bare = false
[branch "branch-tracking-on-clone"]
	remote = fork
	merge = refs/heads/branch-tracking-on-clone
	rebase = interactive
`)

	cfg := NewConfig()
	cfg.Branches["branch-tracking-on-clone"] = &Branch{
		Name:   "branch-tracking-on-clone",
		Remote: "fork",
		Merge:  plumbing.ReferenceName("refs/heads/branch-tracking-on-clone"),
		Rebase: "interactive",
	}

	actual, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(string(actual), Equals, string(expected))
}

func (b *BranchSuite) TestUnmarshal(c *C) {
	input := []byte(`[core]
	bare = false
[branch "branch-tracking-on-clone"]
	remote = fork
	merge = refs/heads/branch-tracking-on-clone
	rebase = interactive
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	c.Assert(err, IsNil)
	branch := cfg.Branches["branch-tracking-on-clone"]
	c.Assert(branch.Name, Equals, "branch-tracking-on-clone")
	c.Assert(branch.Remote, Equals, "fork")
	c.Assert(branch.Merge, Equals, plumbing.ReferenceName("refs/heads/branch-tracking-on-clone"))
	c.Assert(branch.Rebase, Equals, "interactive")
}

func (b *BranchSuite) TestValidateMergeWithPullRef(c *C) {
	// Regression test for https://github.com/go-git/go-git/issues/1871
	// branch.merge should allow refs/pull/<ID>/head (used by GitHub/GitLab PRs)
	prBranch := Branch{
		Name:   "contributor/fix-9999",
		Remote: "upstream",
		Merge:  "refs/pull/9999/head",
	}
	c.Assert(prBranch.Validate(), IsNil)

	mrBranch := Branch{
		Name:   "contributor/fix-42",
		Remote: "origin",
		Merge:  "refs/merge-requests/42/head",
	}
	c.Assert(mrBranch.Validate(), IsNil)
}
