package packp

import (
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// UploadRequest values represent the information transmitted on a
// upload-request message. The zero value is safe to use; Wants, Shallows
// and Capabilities can be populated via append.
type UploadRequest struct {
	Capabilities capability.List
	Wants        []plumbing.Hash
	Shallows     []plumbing.Hash
	Depth        DepthRequest
	Filter       Filter
}

// DepthRequest specifies the depth constraints for a fetch request.
// The zero value means no depth constraint (infinite depth).
//
// Commits cannot be combined with Since or NotRefs (git rejects it).
// Since and NotRefs may be combined to further refine the shallow boundary.
type DepthRequest struct {
	// Deepen limits the fetch to the given number of commits from the tip.
	// Zero means no commit-based depth limit.
	// Corresponds to "deepen <n>" in the protocol.
	Deepen int

	// DeepenSince limits the fetch to commits newer than the given time.
	// Zero value means no time-based limit.
	// Corresponds to "deepen-since <timestamp>" in the protocol.
	DeepenSince time.Time

	// DeepenNot excludes commits reachable from the named references.
	// Multiple refs may be specified. Each emits a "deepen-not <ref>" line.
	DeepenNot []string
}

// IsZero returns true when no depth constraints are set.
func (d DepthRequest) IsZero() bool {
	return d.Deepen == 0 && d.DeepenSince.IsZero() && len(d.DeepenNot) == 0
}
