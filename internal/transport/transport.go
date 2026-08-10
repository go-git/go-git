// Package transport holds transport internals shared across go-git's transport
// implementations (the public facade lives in plumbing/transport, which aliases
// the exported types here). It is not part of go-git's public API.
package transport

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
)

// FetchRequest describes a fetch. It is shared by the v0/v1 and v2 fetch paths;
// plumbing/transport aliases it as transport.FetchRequest.
type FetchRequest struct {
	// Progress is the progress sideband.
	Progress sideband.Progress

	// Wants is the list of object hashes the client wants to fetch.
	// The caller selects which remote refs to fetch (refspec matching)
	// and extracts their hashes.
	Wants []plumbing.Hash

	// Haves is the list of object hashes the client already has.
	// TODO: The transport should compute haves internally from the
	// storer during pack negotiation, matching how canonical git's
	// fetch-pack walks the local object graph to determine common
	// ancestors. Once implemented, remove this field.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// Filter holds the filters to be applied when deciding what
	// objects will be added to the packfile.
	Filter packp.Filter

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool
}
