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
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

// Connection represents a session endpoint connection.
type Connection interface {
	// Close closes the connection.
	Close() error

	// Capabilities returns the list of capabilities supported by the server.
	Capabilities() *capability.List

	// Version returns the Git protocol version the server supports.
	Version() protocol.Version

	// IsStatelessRPC indicates that the connection is a half-duplex connection
	// and should operate in half-duplex mode i.e. performs a single read-write
	// cycle. This fits with the HTTP POST request process where session may
	// read the request, write a response, and exit.
	IsStatelessRPC() bool

	// GetRemoteRefs returns the references advertised by the remote.
	// Using protocol v0 or v1, this returns the references advertised by the
	// remote during the handshake. Using protocol v2, this runs the ls-refs
	// command on the remote.
	// This will error if the session is not already established using
	// Handshake.
	GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)

	// Fetch sends a fetch-pack request to the server.
	Fetch(ctx context.Context, req *FetchRequest) (*FetchResponse, error)

	// Push sends a send-pack request to the server.
	Push(ctx context.Context, req *PushRequest) (*PushResponse, error)
}

var _ io.Closer = Connection(nil)

// ServiceResponse contains the response from the server after a session handshake.
type ServiceResponse struct {
	// Capabilities is the list of capabilities supported by the server.
	Capabilities *capability.List

	// AdvRefs is the list of references advertised by the server.
	// This is only populated if the server is using protocol v0 or v1.
	AdvRefs *packp.AdvRefs

	// Version is the Git protocol version negotiated with the server.
	Version protocol.Version
}

// FetchRequest contains the parameters for a fetch-pack request.
// This is used during the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
type FetchRequest struct {
	// Progress is the progress sideband.
	Progress sideband.Progress

	// Wants is the list of references to fetch.
	Wants []plumbing.Hash

	// Haves is the list of references the client already has.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool

	// StatelessRPC indicates whether the server should use stateless-rpc.
	StatelessRPC bool
}

// FetchResponse contains the response from the server after a fetch request.
type FetchResponse struct {
	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Shallows is the list of shallow references.
	Shallows []plumbing.Hash
}

// PushRequest contains the parameters for a push request.
type PushRequest struct {
	// UpdateRequests is the list of reference update requests.
	packp.UpdateRequests

	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Progress is the progress sideband.
	Progress sideband.Progress
}

// PushResponse contains the response from the server after a push request.
type PushResponse struct {
	// ReportStatus is the status of the reference update requests.
	packp.ReportStatus
}

// PackSession is a Git protocol transfer session.
// This is used by all protocols.
// TODO: rename this to Session.
type PackSession interface {
	// Handshake starts the negotiation with the remote to get version if not
	// already connected.
	// Params are the optional extra parameters to be sent to the server. Use
	// this to send the protocol version of the client and any other extra parameters.
	Handshake(ctx context.Context, forPush bool, params ...string) (Connection, error)
}

// NewPackSession creates a new session that implements a full-duplex Git pack protocol.
func NewPackSession(
	st storage.Storer,
	ep *Endpoint,
	auth AuthMethod,
	cmdr Commander,
) (PackSession, error) {
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

var _ PackSession = &packSession{}

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
			plumbing.NewReferenceFromStrings(name, hash.String()),
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
func (p *packConnection) Fetch(ctx context.Context, req *FetchRequest) (*FetchResponse, error) {
	shupd, err := NegotiatePack(p.st, p, p.r, p.w, req)
	if err != nil {
		return nil, err
	}

	var shallows []plumbing.Hash
	if shupd != nil {
		shallows = shupd.Shallows
	}

	return &FetchResponse{
		Packfile: io.NopCloser(p.r),
		Shallows: shallows,
	}, nil
}

// Push implements Connection.
func (p *packConnection) Push(ctx context.Context, req *PushRequest) (*PushResponse, error) {
	panic("unimplemented")
}
