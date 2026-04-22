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
// The Command method executes a named v2 command, encoding the request
// via req.Encode and decoding the response via resp.Decode. The transport
// layer handles the v2 envelope (command name, capabilities, delim-pkt,
// flush-pkt, and for HTTP, response-end).
type Commander interface {
	Command(ctx context.Context, cmd string, req packp.Encoder, resp packp.Decoder) error
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
	GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)
	Fetch(ctx context.Context, st storage.Storer, req *FetchRequest) error
	Push(ctx context.Context, st storage.Storer, req *PushRequest) error
	Close() error
}
