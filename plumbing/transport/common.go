// Package transport implements the git pack protocol with a pluggable
// This is a low-level package to implement new transports. Use a concrete
// implementation instead (e.g. http, file, ssh).
//
// A simple example of usage can be found in the file package.
package transport

import (
	"context"
	"errors"
	"io"
	"regexp"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/storage"
)

const (
	readErrorSecondsTimeout = 10
)

var (
	// ErrUnsupportedVersion is returned when the protocol version is not
	// supported.
	ErrUnsupportedVersion = errors.New("unsupported protocol version")
	// ErrUnsupportedService is returned when the service is not supported.
	ErrUnsupportedService = errors.New("unsupported service")
	// ErrInvalidResponse is returned when the response is invalid.
	ErrInvalidResponse = errors.New("invalid response")
	// ErrTimeoutExceeded is returned when the timeout is exceeded.
	ErrTimeoutExceeded = errors.New("timeout exceeded")
	// ErrPackedObjectsNotSupported is returned when the server does not support
	// packed objects.
	ErrPackedObjectsNotSupported = errors.New("packed objects not supported")
	// stdErrSkipPattern is used for skipping lines from a command's stderr output.
	// Any line matching this pattern will be skipped from further
	// processing and not be returned to calling code.
	stdErrSkipPattern = regexp.MustCompile("^remote:( =*){0,1}$")
)

// RemoteError represents an error returned by the remote.
// TODO: embed error
type RemoteError struct {
	Reason string
}

// Error implements the error interface.
func (e *RemoteError) Error() string {
	return e.Reason
}

// NewRemoteError creates a new RemoteError.
func NewRemoteError(reason string) error {
	return &RemoteError{Reason: reason}
}

// Connection represents a session endpoint connection.
type Connection interface {
	// Close closes the connection.
	Close() error

	// Capabilities returns the list of capabilities supported by the server.
	Capabilities() *capability.List

	// Version returns the Git protocol version the server supports.
	Version() protocol.Version

	// StatelessRPC indicates that the connection is a half-duplex connection
	// and should operate in half-duplex mode i.e. performs a single read-write
	// cycle. This fits with the HTTP POST request process where session may
	// read the request, write a response, and exit.
	StatelessRPC() bool

	// GetRemoteRefs returns the references advertised by the remote.
	// Using protocol v0 or v1, this returns the references advertised by the
	// remote during the handshake. Using protocol v2, this runs the ls-refs
	// command on the remote.
	// This will error if the session is not already established using
	// Handshake.
	GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error)

	// Fetch sends a fetch-pack request to the server.
	Fetch(ctx context.Context, req *FetchRequest) error

	// Push sends a send-pack request to the server.
	Push(ctx context.Context, req *PushRequest) error
}

var _ io.Closer = Connection(nil)

// FetchRequest contains the parameters for a fetch-pack request.
// This is used during the pack negotiation phase of the fetch operation.
// See https://git-scm.com/docs/pack-protocol#_packfile_negotiation
type FetchRequest struct {
	// Progress is the progress sideband.
	Progress sideband.Progress

	// Wants is the list of references to fetch.
	// TODO: Build this slice in the transport package.
	Wants []plumbing.Hash

	// Haves is the list of references the client already has.
	// TODO: Build this slice in the transport package.
	Haves []plumbing.Hash

	// Depth is the depth of the fetch.
	Depth int

	// IncludeTags indicates whether tags should be fetched.
	IncludeTags bool
}

// PushRequest contains the parameters for a push request.
type PushRequest struct {
	// Packfile is the packfile reader.
	Packfile io.ReadCloser

	// Commands is the list of push commands to be sent to the server.
	// TODO: build the Commands slice in the transport package.
	Commands []*packp.Command

	// Progress is the progress sideband.
	Progress sideband.Progress

	// Options is a set of push-options to be sent to the server during push.
	Options map[string]string

	// Atomic indicates an atomic push.
	// If the server supports atomic push, it will update the refs in one
	// atomic transaction. Either all refs are updated or none.
	Atomic bool
}

// Session is a Git protocol transfer session.
// This is used by all protocols.
type Session interface {
	// Handshake starts the negotiation with the remote to get version if not
	// already connected.
	// Params are the optional extra parameters to be sent to the server. Use
	// this to send the protocol version of the client and any other extra parameters.
	Handshake(ctx context.Context, service Service, params ...string) (Connection, error)
}

// Commander creates Command instances. This is the main entry point for
// transport implementations.
type Commander interface {
	// Connect creates a new Command for the given git command and
	// endpoint. cmd can be git-upload-pack or git-receive-pack. An
	// error should be returned if the endpoint is not supported or the
	// command cannot be created (e.g. binary does not exist, connection
	// cannot be established).
	Command(ctx context.Context, cmd string, ep *Endpoint, auth AuthMethod, params ...string) (Command, error)
}

// Command is used for a single command execution.
// This interface is modeled after exec.Cmd and ssh.Session in the standard
// library.
type Command interface {
	// StderrPipe returns a pipe that will be connected to the command's
	// standard error when the command starts. It should not be called after
	// Start.
	StderrPipe() (io.Reader, error)
	// StdinPipe returns a pipe that will be connected to the command's
	// standard input when the command starts. It should not be called after
	// Start. The pipe should be closed when no more input is expected.
	StdinPipe() (io.WriteCloser, error)
	// StdoutPipe returns a pipe that will be connected to the command's
	// standard output when the command starts. It should not be called after
	// Start.
	StdoutPipe() (io.Reader, error)
	// Start starts the specified command. It does not wait for it to
	// complete.
	Start() error
	// Close closes the command and releases any resources used by it. It
	// will block until the command exits.
	Close() error
}

// CommandKiller expands the Command interface, enabling it for being killed.
type CommandKiller interface {
	// Kill and close the session whatever the state it is. It will block until
	// the command is terminated.
	Kill() error
}

type client struct {
	cmdr Commander
}

// NewPackTransport creates a new client using the given Commander.
func NewPackTransport(runner Commander) Transport {
	return &client{runner}
}

// NewSession returns a new session for an endpoint.
func (c *client) NewSession(st storage.Storer, ep *Endpoint, auth AuthMethod) (Session, error) {
	return NewPackSession(st, ep, auth, c.cmdr)
}

// SupportedProtocols returns a list of supported Git protocol versions by
// the transport client.
func (c *client) SupportedProtocols() []protocol.Version {
	return []protocol.Version{
		protocol.V0,
		protocol.V1,
	}
}
