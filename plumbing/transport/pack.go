package transport

import (
	"bufio"
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/utils/ioutil"
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

// readStderr is a helper function that reads the stderr of a command and
// returns an error if it's not empty.
func readStderr(r io.Reader) error {
	bts, err := io.ReadAll(r)
	if err == nil && len(bts) > 0 {
		return NewRemoteError(string(bts))
	}
	return nil
}

// Handshake implements Session.
func (p *packSession) Handshake(ctx context.Context, forPush bool, params ...string) (conn Connection, err error) {
	service := UploadPackServiceName
	if forPush {
		service = ReceivePackServiceName
	}

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

	cr := ioutil.NewContextReaderWithCloser(ctx, stdout, cmd)
	c.r = bufio.NewReader(cr)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	c.e = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// If we receive an EOF error, we need to check if there is any error
	// message in the stderr.
	defer func() {
		if errors.Is(err, io.EOF) {
			if rerr := readStderr(stderr); rerr != nil {
				err = rerr
			}
		}
	}()

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

	return p.refs.References, nil
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
