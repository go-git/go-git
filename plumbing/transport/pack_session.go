package transport

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
)

// Commander is an optional capability that Protocol v2-capable sessions
// implement. It provides access to arbitrary v2 commands beyond the
// built-in Fetch and Push operations.
//
// Sessions that negotiate Protocol v2 (version 2) implement this interface.
// The Command method executes a named v2 command: req carries the
// command-specific arguments and is encoded into the request, while resp
// decodes the response. For example, GetRemoteRefs runs
// Command(ctx, "ls-refs", lsRefsArgs, lsRefsOutput). The session builds the v2
// request envelope (command name, the capabilities collected during the
// handshake, delim-pkt, the arguments, and flush-pkt) and, for HTTP, handles
// the response-end packet.
type Commander interface {
	Command(ctx context.Context, cmd string, req packp.CommandArgs, resp packp.Decoder) error
}

// Transport is implemented by transports that speak the Git pack
// protocol. Each transport implements this directly — stream transports
// use the NewStreamSession helper, HTTP handles smart/dumb internally.
type Transport interface {
	Handshake(ctx context.Context, req *Request) (Session, error)
}

// Session is returned by Transport.Handshake.
type Session interface {
	Capabilities() *capability.List
	GetRemoteRefs(ctx context.Context, opts *GetRemoteRefsOptions) (*RemoteRefs, error)
	Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error
	Push(ctx context.Context, st storage.Storer, req *PushRequest) error
	Close() error
}

// GetRemoteRefsOptions configures Session.GetRemoteRefs. A nil pointer
// requests all references with default behavior, matching git's
// transport_get_remote_refs(transport, NULL).
type GetRemoteRefsOptions struct {
	// RefPrefixes limits the returned references to those matching the
	// given prefixes. For Protocol v2 these map directly to ls-refs
	// ref-prefix arguments. For v0/v1 the server always advertises every
	// reference, so prefixes are ignored.
	RefPrefixes []string
}

// RemoteRefs holds the result of Session.GetRemoteRefs. It is a struct so
// that new output fields can be added without changing the interface.
type RemoteRefs struct {
	// References are the advertised references, with HEAD resolved to a
	// symbolic reference when the server reports a symref target.
	References []*plumbing.Reference
	// Unborn is the symref target of HEAD when HEAD points at an unborn
	// branch. It is empty when HEAD is not unborn or the server does not
	// report it (v0/v1).
	Unborn plumbing.ReferenceName
}

// NewRemoteRefs builds a RemoteRefs from a resolved reference list,
// detecting an unborn HEAD: a symbolic HEAD whose target has no
// corresponding hash reference in the advertisement.
func NewRemoteRefs(refs []*plumbing.Reference) *RemoteRefs {
	// A detached remote HEAD is advertised as a bare hash. The v0/v1
	// advertisement resolves it to a symbolic HEAD during decode; the v2
	// ls-refs path does not, so apply the same hash→branch heuristic here so a
	// clone records a symbolic HEAD rather than a detached one (matching git).
	for i, ref := range refs {
		if ref.Name() == plumbing.HEAD && ref.Type() == plumbing.HashReference {
			refs[i] = packp.ResolveHeadFromHashHeuristic(ref, refs)
			break
		}
	}

	rr := &RemoteRefs{References: refs}

	var headTarget plumbing.ReferenceName
	hashRefs := make(map[plumbing.ReferenceName]struct{}, len(refs))
	for _, ref := range refs {
		switch {
		case ref.Type() == plumbing.HashReference:
			hashRefs[ref.Name()] = struct{}{}
		case ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference:
			headTarget = ref.Target()
		}
	}

	if headTarget != "" {
		if _, ok := hashRefs[headTarget]; !ok {
			rr.Unborn = headTarget
		}
	}

	return rr
}
