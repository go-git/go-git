package transport

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// NewPackSession creates a new session that implements a full-duplex Git pack protocol.
func NewPackSession(
	st storage.Storer,
	ep *Endpoint,
	auth AuthMethod,
	cmdr Commander,
) (Session, error) {
	ps := &PackSession{
		ep:   ep,
		auth: auth,
		cmdr: cmdr,
		st:   st,
	}
	return ps, nil
}

// PackSession is a session that implements a full-duplex Git pack transport.
type PackSession struct {
	cmdr Commander
	ep   *Endpoint
	auth AuthMethod
	st   storage.Storer
}

var _ Session = &PackSession{}

// Handshake implements Session.
func (p *PackSession) Handshake(ctx context.Context, service Service, params ...string) (conn Connection, err error) {
	switch service {
	case UploadPackService, ReceivePackService:
		// do nothing
	default:
		return nil, ErrUnsupportedService
	}
	cmd, err := p.cmdr.Command(ctx, service.String(), p.ep, p.auth, params...)
	if err != nil {
		return nil, err
	}

	c := &packConnection{
		st:  p.st,
		cmd: cmd,
		svc: service,
	}

	// Check if the context is already done before starting the command.
	if ctx.Err() != nil {
		return nil, ctx.Err()
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

	cr := ioutil.NewContextReaderWithCloser(ctx, stdout, cmd)
	c.r = bufio.NewReader(cr)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	// Some transports like Git doesn't support stderr, so we need to check if
	// it's not nil before starting to read it.
	if stderr != nil {
		go io.Copy(&c.stderrBuf, stderr) // nolint: errcheck
	}

	// Check if stderr is not empty before returning.
	defer func() { checkError(c.stderr(), &err) }()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c.version, err = DiscoverVersion(c.r)
	if err != nil {
		return nil, err
	}

	switch c.version {
	case protocol.V2:
		return nil, ErrUnsupportedVersion
	case protocol.V1:
		// Read the version line
		fallthrough
	case protocol.V0:
	}

	ar := packp.NewAdvRefs()
	if err := ar.Decode(c.r); err != nil {
		return nil, err
	}

	c.refs = ar
	c.caps = ar.Capabilities

	return c, nil
}

// packConnection is a convenience type that implements io.ReadWriteCloser.
type packConnection struct {
	st        storage.Storer
	cmd       Command
	svc       Service
	w         io.WriteCloser // stdin
	r         *bufio.Reader  // stdout
	stderrBuf bytes.Buffer

	version protocol.Version
	caps    *capability.List
	refs    *packp.AdvRefs
}

var _ Connection = &packConnection{}

// stderr returns stderr of the command if it's not empty. This will always
// return a RemoteError.
func (p *packConnection) stderr() error {
	s := strings.TrimSpace(p.stderrBuf.String())
	if s == "" {
		return nil
	}

	return NewRemoteError(s)
}

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

	// Some servers like jGit, announce capabilities instead of returning an
	// packp message with a flush. This verifies that we received a empty
	// adv-refs, even if it contains capabilities.
	forPush := p.svc == ReceivePackService
	if !forPush && p.refs.IsEmpty() {
		return nil, ErrEmptyRemoteRepository
	}

	return p.refs.MakeReferenceSlice()
}

// Version implements Connection.
func (p *packConnection) Version() protocol.Version {
	return p.version
}

// StatelessRPC implements Connection.
func (*packConnection) StatelessRPC() bool {
	return false
}

// Fetch implements Connection.
func (p *packConnection) Fetch(ctx context.Context, req *FetchRequest) (err error) {
	shallows, err := NegotiatePack(ctx, p.st, p, p.r, p.w, req)
	if err != nil {
		return err
	}

	return FetchPack(ctx, p.st, p, io.NopCloser(p.r), shallows, req)
}

// Push implements Connection.
func (p *packConnection) Push(ctx context.Context, req *PushRequest) (err error) {
	return SendPack(ctx, p.st, p, p.w, io.NopCloser(p.r), req)
}

// checkError checks if the error is not nil updates the pointer with the
// error.
func checkError(err error, perr *error) {
	if err != nil {
		*perr = err
	}
}
