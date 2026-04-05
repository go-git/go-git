// Package git implements the Git TCP transport for the new transport API.
package git

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// DefaultPort is the default port for the git protocol.
const DefaultPort = 9418

// Options configures the Git TCP transport.
type Options struct {
	// DialContext is the function used to establish TCP connections.
	// If nil, net.Dialer{}.DialContext is used.
	DialContext transport.DialContextFunc

	// DialProxy wraps DialContext to route connections through a proxy.
	// If nil, connections are made directly.
	DialProxy func(transport.DialContextFunc) transport.DialContextFunc
}

// Transport implements the git:// transport protocol.
type Transport struct {
	opts Options
}

// NewTransport creates a Git TCP transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

// Connect opens a raw connection to the Git protocol server.
func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}
	addr := net.JoinHostPort(host, port)

	dialFn := t.opts.DialContext
	if dialFn == nil {
		dialFn = (&net.Dialer{}).DialContext
	}

	if t.opts.DialProxy != nil {
		dialFn = t.opts.DialProxy(dialFn)
	}

	nc, err := dialFn(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	proto := packp.GitProtoRequest{
		RequestCommand: req.Command,
		Pathname:       req.URL.Path,
		Host:           net.JoinHostPort(host, port),
	}

	if gp := transport.GitProtocolEnv(req.Protocol); gp != "" {
		proto.ExtraParams = append(proto.ExtraParams, gp)
	}

	if err := proto.Encode(nc); err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("git: encode proto request: %w", err)
	}

	return &gitConn{nc: nc}, nil
}

// gitConn implements transport.Conn over a TCP connection.
type gitConn struct {
	nc net.Conn
}

var _ transport.Conn = (*gitConn)(nil)

func (c *gitConn) Reader() io.Reader      { return c.nc }
func (c *gitConn) Writer() io.WriteCloser { return ioutil.WriteNopCloser(c.nc) }
func (c *gitConn) Close() error           { return c.nc.Close() }
