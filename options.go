package git

import (
	"errors"

	"gopkg.in/src-d/go-git.v3/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
)

const (
	// DefaultRemoteName name of the default Remote, just like git command
	DefaultRemoteName = "origin"
)

var (
	ErrMissingURL        = errors.New("URL field is required")
	ErrMissingReferences = errors.New("references cannot be empty")
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

// Validate validate the fields and set the default values
func (o *RepositoryCloneOptions) Validate() error {
	if o.URL == "" {
		return ErrMissingURL
	}

	if o.RemoteName == "" {
		o.RemoteName = DefaultRemoteName
	}

	if o.ReferenceName == "" {
		o.ReferenceName = core.HEAD
	}

	return nil
}

// RepositoryPullOptions describe how a pull should be perform
type RepositoryPullOptions struct {
	// Name of the remote to be pulled
	RemoteName string
	// Remote branch to clone
	ReferenceName core.ReferenceName
	// Fetch only ReferenceName if true
	SingleBranch bool
	// Limit fetching to the specified number of commits
	Depth int
}

// Validate validate the fields and set the default values
func (o *RepositoryPullOptions) Validate() error {
	if o.RemoteName == "" {
		o.RemoteName = DefaultRemoteName
	}

	if o.ReferenceName == "" {
		o.ReferenceName = core.HEAD
	}

	return nil
}

// RemoteFetchOptions describe how a fetch should be perform
type RemoteFetchOptions struct {
	// Remote branchs to fetch
	References []*core.Reference
	// Local references present on the local storage
	LocalReferences []*core.Reference
	// Limit fetching to the specified number of commits
	Depth int
}

// Validate validate the fields and set the default values
func (o *RemoteFetchOptions) Validate() error {
	if len(o.References) == 0 {
		return ErrMissingReferences
	}

	return nil
}
