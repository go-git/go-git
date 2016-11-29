package packp

import (
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
)

// UploadRequest values represent the information transmitted on a
// upload-request message.  Values from this type are not zero-value
// safe, use the New function instead.
// This is a low level type, use UploadPackRequest instead.
type UploadRequest struct {
	Capabilities *capability.List
	Wants        []plumbing.Hash
	Shallows     []plumbing.Hash
	Depth        Depth
}

// Depth values stores the desired depth of the requested packfile: see
// DepthCommit, DepthSince and DepthReference.
type Depth interface {
	isDepth()
}

// DepthCommits values stores the maximum number of requested commits in
// the packfile.  Zero means infinite.  A negative value will have
// undefined consecuences.
type DepthCommits int

func (d DepthCommits) isDepth() {}

// DepthSince values requests only commits newer than the specified time.
type DepthSince time.Time

func (d DepthSince) isDepth() {}

// DepthReference requests only commits not to found in the specified reference.
type DepthReference string

func (d DepthReference) isDepth() {}

// NewUploadRequest returns a pointer to a new UlReq value, ready to be used.  It has
// no capabilities, wants or shallows and an infinite depth.  Please
// note that to encode an upload-request it has to have at least one
// wanted hash.
func NewUploadRequest() *UploadRequest {
	return &UploadRequest{
		Capabilities: capability.NewList(),
		Wants:        []plumbing.Hash{},
		Shallows:     []plumbing.Hash{},
		Depth:        DepthCommits(0),
	}
}

// Want adds a hash reference to the 'wants' list.
func (r *UploadRequest) Want(h ...plumbing.Hash) {
	r.Wants = append(r.Wants, h...)
}
