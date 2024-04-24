package transport

import (
	"bufio"
	"context"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"
)

// packConnection is a convenience type that implements io.ReadWriteCloser.
type packConnection struct {
	st  storage.Storer
	cmd Command
	w   io.WriteCloser // stdin
	r   *bufio.Reader  // stdout
	e   io.Reader      // stderr

	version protocol.Version
	caps    *capability.List
	refs    *packp.AdvRefs
}

var _ Connection = &packConnection{}

// Close implements Connection.
func (p *packConnection) Close() error {
	return p.cmd.Close()
}

// Capabilities implements Connection.
func (p *packConnection) Capabilities() *capability.List {
	return p.caps
}

// GetRemoteRefs implements Connection.
func (p *packConnection) GetRemoteRefs(ctx context.Context) (memory.ReferenceStorage, map[string]plumbing.Hash, error) {
	if p.refs == nil {
		// TODO: return appropriate error
		return nil, nil, ErrEmptyRemoteRepository
	}

	refs, err := p.refs.AllReferences()
	if err != nil {
		return nil, nil, err
	}

	return refs, p.refs.Peeled, nil
}

// Version implements Connection.
func (p *packConnection) Version() protocol.Version {
	return p.version
}

// IsStatelessRPC implements Connection.
func (*packConnection) IsStatelessRPC() bool {
	return false
}

// Fetch implements Connection.
func (p *packConnection) Fetch(ctx context.Context, req *FetchRequest) (*FetchResponse, error) {
	shupd, err := NegotiatePack(p.st, p, p.w, p.r, req)
	if err != nil {
		return nil, err
	}

	return &FetchResponse{
		Packfile: io.NopCloser(p.r),
		Shallows: shupd.Shallows,
	}, nil
}

// Push implements Connection.
func (p *packConnection) Push(ctx context.Context, req *PushRequest) (*PushResponse, error) {
	panic("unimplemented")
}
