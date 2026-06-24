package transport

import (
	"context"
	"io"
	"net"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
)

// DialContextFunc is the function signature for dialing network connections.
// It also implements proxy.Dialer and proxy.ContextDialer so it can be
// passed directly to proxy.FromURL without an adapter.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Dial implements proxy.Dialer.
func (f DialContextFunc) Dial(network, addr string) (net.Conn, error) {
	return f(context.Background(), network, addr)
}

// DialContext implements proxy.ContextDialer.
func (f DialContextFunc) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return f(ctx, network, addr)
}

// RemoteError represents an error returned by the remote.
// TODO: embed error
type RemoteError struct {
	Reason string
}

// Error implements the error interface.
func (e *RemoteError) Error() string {
	return e.Reason
}

// NewRemoteError creates a new RemoteError.
func NewRemoteError(reason string) error {
	return &RemoteError{Reason: reason}
}

// RefsRequest contains the parameters for remote reference discovery,
// mirroring how Fetch and Push take a request value.
type RefsRequest struct {
	// Prefixes optionally restricts the advertisement to references whose
	// name begins with one of the given prefixes (the protocol v2 ls-refs
	// "ref-prefix" feature). It is an optimization hint only: a server may
	// still return references outside these prefixes, so callers must filter
	// the result regardless. A nil or empty slice requests the full
	// advertisement. Transports that cannot scope the advertisement
	// (protocol v0/v1) ignore this field.
	Prefixes []string

	// DefaultBranch is the client's configured default branch
	// (init.defaultBranch, a short name such as "main"). It is preferred when
	// resolving a detached remote HEAD to a symbolic reference, matching
	// upstream's guess_remote_head. Empty falls back to refs/heads/master then
	// the first matching ref.
	DefaultBranch string
}

// DefaultBranchRef returns the full branch ref name of req's configured default
// branch, or the empty ref name when req is nil or unset.
func DefaultBranchRef(req *RefsRequest) plumbing.ReferenceName {
	if req == nil || req.DefaultBranch == "" {
		return ""
	}
	if name := plumbing.ReferenceName(req.DefaultBranch); name.IsBranch() {
		return name
	}
	return plumbing.NewBranchReferenceName(req.DefaultBranch)
}

// FetchRequest contains the parameters for a fetch-pack request.
// This is used during the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
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

// PushRequest contains the parameters for a push request.
type PushRequest struct {
	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Commands is the list of ref update commands to send to the server.
	// The caller builds these from refspec matching against local and
	// remote refs, including force-push validation and fast-forward
	// checks. This matches canonical git's send-pack, which also
	// receives pre-built commands from the caller.
	Commands []*packp.Command

	// Progress is the progress sideband.
	Progress sideband.Progress

	// Options is a set of push-options to be sent to the server during push.
	Options []string

	// Atomic indicates an atomic push.
	// If the server supports atomic push, it will update the refs in one
	// atomic transaction. Either all refs are updated or none.
	Atomic bool

	// Quiet indicates whether the server should suppress human-readable
	// output.
	Quiet bool
}
