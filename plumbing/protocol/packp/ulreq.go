package packp

import (
	"fmt"
	"strconv"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

// UploadRequest values represent the information transmitted on a
// upload-request message.  Values from this type are not zero-value
// safe, use the New function instead.
type UploadRequest struct {
	Capabilities *capability.List
	Wants        []plumbing.Hash
	Shallows     []plumbing.Hash
	Depth        Depth
	Filter       Filter
}

// Depth values stores the desired depth of the requested packfile: see
// DepthCommit, DepthSince and DepthReference.
type Depth interface {
	fmt.Stringer
	IsZero() bool
}

// DepthCommits values stores the maximum number of requested commits in
// the packfile.  Zero means infinite.  A negative value will have
// undefined consequences.
type DepthCommits int

// IsZero returns true if the depth is zero.
func (d DepthCommits) IsZero() bool {
	return d == 0
}

func (d DepthCommits) String() string {
	return strconv.Itoa(int(d))
}

// DepthSince values requests only commits newer than the specified time.
type DepthSince time.Time

// IsZero returns true if the time is zero.
func (d DepthSince) IsZero() bool {
	return time.Time(d).IsZero()
}

func (d DepthSince) String() string {
	return time.Time(d).Format(time.RFC3339)
}

// DepthReference requests only commits not to found in the specified reference.
type DepthReference string

// IsZero returns true if the reference is empty.
func (d DepthReference) IsZero() bool {
	return string(d) == ""
}

func (d DepthReference) String() string {
	return string(d)
}

// NewUploadRequest returns a pointer to a new UploadRequest value, ready to be
// used. It has no capabilities, wants or shallows and an infinite depth. Please
// note that to encode an upload-request it has to have at least one wanted hash.
func NewUploadRequest() *UploadRequest {
	return &UploadRequest{
		Capabilities: capability.NewList(),
		Wants:        []plumbing.Hash{},
		Shallows:     []plumbing.Hash{},
		Depth:        DepthCommits(0),
	}
}
