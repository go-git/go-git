package config

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type BranchSuite struct {
	suite.Suite
}

func TestBranchSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(BranchSuite))
}

func (b *BranchSuite) TestValidateName() {
	goodBranch := Branch{
		Name:   "master",
		Remote: "some_remote",
		Merge:  "refs/heads/master",
	}
	badBranch := Branch{
		Remote: "some_remote",
		Merge:  "refs/heads/master",
	}
	b.Nil(goodBranch.Validate())
	b.NotNil(badBranch.Validate())
}

func (b *BranchSuite) TestValidateMerge() {
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
	b.Nil(goodBranch.Validate())
	b.NotNil(badBranch.Validate())
}

func (b *BranchSuite) TestMarshal() {
	expected := []byte(`[core]
	bare = false
	filemode = true
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
	b.NoError(err)
	b.Equal(string(expected), string(actual))
}

func (b *BranchSuite) TestUnmarshal() {
	input := []byte(`[core]
	bare = false
[branch "branch-tracking-on-clone"]
	remote = fork
	merge = refs/heads/branch-tracking-on-clone
	rebase = interactive
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	b.NoError(err)
	branch := cfg.Branches["branch-tracking-on-clone"]
	b.Equal("branch-tracking-on-clone", branch.Name)
	b.Equal("fork", branch.Remote)
	b.Equal(plumbing.ReferenceName("refs/heads/branch-tracking-on-clone"), branch.Merge)
	b.Equal("interactive", branch.Rebase)
}

func (b *BranchSuite) TestValidateMergeWithPullRef() {
	// Regression test for https://github.com/go-git/go-git/issues/1871
	// branch.merge should allow refs/pull/<ID>/head (used by GitHub/GitLab PRs)
	prBranch := Branch{
		Name:   "contributor/fix-9999",
		Remote: "upstream",
		Merge:  "refs/pull/9999/head",
	}
	b.Nil(prBranch.Validate())

	mrBranch := Branch{
		Name:   "contributor/fix-42",
		Remote: "origin",
		Merge:  "refs/merge-requests/42/head",
	}
	b.Nil(mrBranch.Validate())
}
