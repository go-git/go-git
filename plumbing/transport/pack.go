package transport

import (
	"bufio"
	"context"
	"io"
	"log"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage"
)

// NewPackSession creates a new session that implements a full-duplex Git pack protocol.
func NewPackSession(
	st storage.Storer,
	ep *Endpoint,
	auth AuthMethod,
	cmdr Commander,
) (Session, error) {
	ps := &packSession{
		ep:   ep,
		auth: auth,
		cmdr: cmdr,
		st:   st,
	}
	return ps, nil
}

type packSession struct {
	cmdr Commander
	ep   *Endpoint
	auth AuthMethod
	st   storage.Storer
}

var _ Session = &packSession{}

// Handshake implements Session.
func (p *packSession) Handshake(ctx context.Context, forPush bool, params ...string) (Connection, error) {
	service := UploadPackServiceName
	if forPush {
		service = ReceivePackServiceName
	}

	log.Printf("handshake: service=%s", service)
	cmd, err := p.cmdr.Command(ctx, service, p.ep, p.auth, params...)
	if err != nil {
		return nil, err
	}

	c := &packConnection{
		st:  p.st,
		cmd: cmd,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	c.w = stdin

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	c.r = bufio.NewReader(stdout)

	// TODO: listen for errors in stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	c.e = stderr

	log.Printf("open conn: starting command")
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	log.Printf("open conn: command started")

	c.version, err = DiscoverVersion(c.r)
	if err != nil {
		return nil, err
	}

	switch c.version {
	case protocol.VersionV2:
		return nil, ErrUnsupportedVersion
	case protocol.VersionV1:
		// Read the version line
		fallthrough
	case protocol.VersionV0:
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(c.r); err != nil {
		return nil, err
	}

	c.refs = ar
	c.caps = ar.Capabilities

	// Some servers like jGit, announce capabilities instead of returning an
	// packp message with a flush. This verifies that we received a empty
	// adv-refs, even it contains capabilities.
	if !forPush && ar.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	FilterUnsupportedCapabilities(ar.Capabilities)

	log.Printf("open conn: success")

	return c, nil
}

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
func (p *packConnection) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	if p.refs == nil {
		// TODO: return appropriate error
		return nil, ErrEmptyRemoteRepository
	}

	refs, err := p.refs.AllReferences()
	if err != nil {
		return nil, err
	}

	// FIXME: this is a bit of a hack, to fix this, we need to redefine and
	// simplify AdvRefs.
	var allRefs []*plumbing.Reference
	for _, ref := range refs {
		allRefs = append(allRefs, ref)
	}
	for name, hash := range p.refs.Peeled {
		allRefs = append(allRefs,
			plumbing.NewReferenceFromStrings(name+"^{}", hash.String()),
		)
	}

	return allRefs, nil
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
func (p *packConnection) Fetch(ctx context.Context, req *FetchRequest) error {
	shallows, err := NegotiatePack(p.st, p, p.r, p.w, req)
	if err != nil {
		return err
	}

	return FetchPack(ctx, p.st, p, io.NopCloser(p.r), shallows, req)
}

// Push implements Connection.
func (p *packConnection) Push(ctx context.Context, req *PushRequest) error {
	return SendPack(ctx, p.st, p, p.w, io.NopCloser(p.r), req)
}
