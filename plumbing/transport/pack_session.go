package transport

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
)

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
