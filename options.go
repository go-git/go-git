package git

import (
	"gopkg.in/src-d/go-git.v3/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
)

const (
	// DefaultRemoteName name of the default Remote, just like git command
	DefaultRemoteName = "origin"
)

// RepositoryCloneOptions describe how a clone should be perform
type RepositoryCloneOptions struct {
	// The (possibly remote) repository URL to clone from
	URL string
	// Auth credentials, if required, to uses with the remote repository
	Auth common.AuthMethod
	// Name of the remote to be added, by default `origin`
	RemoteName string
	// Remote branch to clone
	ReferenceName core.ReferenceName
	// Fetch only ReferenceName if true
	SingleBranch bool
	// Limit fetching to the specified number of commits
	Depth int
}

func (o *RepositoryCloneOptions) Default() {
	if o.RemoteName == "" {
		o.RemoteName = DefaultRemoteName
	}

	if o.ReferenceName == "" {
		o.ReferenceName = core.HEAD
	}
}

// RemoteFetchOptions describe how a fetch should be perform
type RemoteFetchOptions struct {
	// Remote branchs to fetch
	References []*core.Reference
	// Limit fetching to the specified number of commits
	Depth int
}
