package rad

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

// Handshake performs a pack protocol handshake for req, delegating to the
// embedded file transport. It returns transport.ErrCommandUnsupported for
// git-receive-pack and git-upload-archive.
func (t *Transport) Handshake(ctx context.Context, req *transport.Request) (transport.Session, error) {
	if unsupported(req.Command) {
		return nil, fmt.Errorf("%w: %s", transport.ErrCommandUnsupported, req.Command)
	}
	return t.file.Handshake(ctx, req)
}

var (
	_ transport.Transport = (*Transport)(nil)
	_ transport.Connector = (*Transport)(nil)
)
