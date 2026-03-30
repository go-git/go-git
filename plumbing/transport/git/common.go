// Package git implements the git transport protocol.
package git

import (
	"context"
	"io"
	"net"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func init() {
	transport.Register("git", DefaultClient)
}

// DefaultClient is the default git client.
var DefaultClient = transport.NewPackTransport(&runner{})

// DefaultPort is the default port for the git protocol.
const DefaultPort = 9418

type runner struct{}

// Command returns a new Command for the given cmd in the given Endpoint
func (r *runner) Command(_ context.Context, cmd string, ep *transport.Endpoint, _ transport.AuthMethod, params ...string) (transport.Command, error) {
	c := &command{command: cmd, endpoint: ep, params: params}
	if err := c.connect(); err != nil {
		return nil, err
	}

	return c, nil
}

type command struct {
	conn      net.Conn
	connected bool
	command   string
	endpoint  *transport.Endpoint
	params    []string
}

// Start executes the command sending the required message to the TCP connection
func (c *command) Start() error {
	req := packp.GitProtoRequest{
		RequestCommand: c.command,
		Pathname:       c.endpoint.Path,
		ExtraParams:    c.params,
	}

	req.Host = c.getHostWithPort()

	return req.Encode(c.conn)
}

func (c *command) connect() error {
	if c.connected {
		return transport.ErrAlreadyConnected
	}

	var err error
	c.conn, err = net.Dial("tcp", c.getHostWithPort())
	if err != nil {
		return err
	}

	c.connected = true
	return nil
}

func (c *command) getHostWithPort() string {
	host := c.endpoint.Host
	port := c.endpoint.Port()
	if port == "" {
		return net.JoinHostPort(host, strconv.Itoa(DefaultPort))
	}

	return host
}

// StderrPipe git protocol doesn't have any dedicated error channel
func (c *command) StderrPipe() (io.Reader, error) {
	return nil, nil
}

// StdinPipe returns the underlying connection as WriteCloser, wrapped to prevent
// call to the Close function from the connection, a command execution in git
// protocol can't be closed or killed
func (c *command) StdinPipe() (io.WriteCloser, error) {
	return ioutil.WriteNopCloser(c.conn), nil
}

// StdoutPipe returns the underlying connection as Reader
func (c *command) StdoutPipe() (io.Reader, error) {
	return c.conn, nil
}

// Close closes the TCP connection and connection.
func (c *command) Close() error {
	if !c.connected {
		return nil
	}

	c.connected = false
	return c.conn.Close()
}
