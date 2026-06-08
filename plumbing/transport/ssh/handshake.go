package ssh

import (
	"context"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// Handshake implements transport.Transport.
func (t *Transport) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	conn, err := t.Connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewStreamSession(conn, req.Command)
}

var (
	_ transport.Transport = (*Transport)(nil)
	_ transport.Connector = (*Transport)(nil)
)
