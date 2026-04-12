package packp

import (
	"fmt"
	"strconv"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// UploadRequest values represent the information transmitted on a
// upload-request message. The zero value is safe to use; Wants, Shallows
// and Capabilities can be populated via append. Depth defaults to
// infinite (no depth limit) when nil.
type UploadRequest struct {
	Capabilities capability.List
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
