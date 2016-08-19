package git

import (
	"errors"
	"fmt"

	"gopkg.in/src-d/go-git.v3/clients/common"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
)

const (
	// DefaultRemoteName name of the default Remote, just like git command
	DefaultRemoteName          = "origin"
	DefaultSingleBranchRefSpec = "+refs/heads/%s:refs/remotes/%s/%[1]s"
	DefaultRefSpec             = "+refs/heads/*:refs/remotes/%s/*"
)

var (
	ErrMissingURL     = errors.New("URL field is required")
	ErrInvalidRefSpec = errors.New("invalid refspec")
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

func (o *RepositoryCloneOptions) refSpec(s core.ReferenceStorage) (config.RefSpec, error) {
	var spec string
	if o.SingleBranch {
		head, err := core.ResolveReference(s, o.ReferenceName)
		if err != nil {
			return "", err
		}

		spec = fmt.Sprintf(DefaultSingleBranchRefSpec, head.Name().Short(), o.RemoteName)
	} else {
		spec = fmt.Sprintf(DefaultRefSpec, o.RemoteName)
	}

	return config.RefSpec(spec), nil
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
	RefSpec config.RefSpec
	Depth   int
}

// Validate validate the fields and set the default values
func (o *RemoteFetchOptions) Validate() error {
	if !o.RefSpec.IsValid() {
		return ErrInvalidRefSpec
	}

	return nil
}
