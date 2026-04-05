// Package ssh implements the SSH transport for the new transport API.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/kevinburke/ssh_config"
	gossh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/utils/ioutil"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/sshagent"
)

// DefaultPort is the default port for the SSH protocol.
const DefaultPort = 22

// DefaultUsername is the default username for SSH connections.
const DefaultUsername = "git"

// DefaultSSHConfig is the reader used to access parameters stored in the
// system's ssh_config files. If nil all the ssh_config are ignored.
var DefaultSSHConfig Config = ssh_config.DefaultUserSettings

// Config is a reader of SSH configuration.
type Config interface {
	Get(alias, key string) string
}

// DefaultAuthBuilder is the function used to create a default ClientConfig
// when Options.ClientConfig is nil. It uses SSH agent authentication.
var DefaultAuthBuilder = func(user string) (*gossh.ClientConfig, error) {
	a, _, err := sshagent.New()
	if err != nil {
		return nil, err
	}

	if user == "" {
		user = DefaultUsername
	}

	return &gossh.ClientConfig{
		User: user,
		Auth: []gossh.AuthMethod{gossh.PublicKeysCallback(a.Signers)},
	}, nil
}

// Options configures the SSH transport.
type Options struct {
	// ClientConfig provides SSH client configuration for each request.
	// If nil, DefaultAuthBuilder is used with the username from the URL.
	ClientConfig func(context.Context, *transport.Request) (*gossh.ClientConfig, error)

	// DialContext is the function used to establish TCP connections.
	// If nil, a default net.Dialer is used.
	DialContext transport.DialContextFunc

	// DialProxy wraps DialContext to route connections through a proxy.
	// If nil, connections are made directly.
	DialProxy func(transport.DialContextFunc) transport.DialContextFunc
}

// Transport implements the ssh:// transport protocol.
type Transport struct {
	opts Options
}

// NewTransport creates an SSH transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

// Connect implements transport.Connectable.
func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	conn, err := t.connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return transport.NewConn(conn.stdout, conn.stdin, conn.Close), nil
}

func (t *Transport) connect(ctx context.Context, req *transport.Request) (*sshConn, error) {
	config, err := t.resolveConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	hostWithPort := resolveHostWithPort(req)

	// Set up host key verification from known_hosts if not provided.
	if config.HostKeyCallback == nil {
		db, err := newKnownHostsDb()
		if err != nil {
			return nil, err
		}
		config.HostKeyCallback = db.HostKeyCallback()
		config.HostKeyAlgorithms = db.HostKeyAlgorithms(hostWithPort)
	} else if len(config.HostKeyAlgorithms) == 0 {
		db, err := newKnownHostsDb()
		if err != nil {
			return nil, err
		}
		config.HostKeyAlgorithms = db.HostKeyAlgorithms(hostWithPort)
	}

	client, err := t.dial(ctx, "tcp", hostWithPort, config)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	gitProtocol := transport.GitProtocolEnv(req.Protocol)
	if gitProtocol != "" {
		_ = session.Setenv("GIT_PROTOCOL", gitProtocol)
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	conn := &sshConn{
		stdout:  stdoutPipe,
		stdin:   stdinPipe,
		session: session,
		client:  client,
	}

	// Read stderr in background.
	go func() {
		var buf bytes.Buffer
		_, _ = ioutil.CopyBufferPool(&buf, stderrPipe)
		conn.stderrBuf.Store(&buf)
	}()

	cmd := buildCommand(req)
	if err := session.Start(cmd); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}

	return conn, nil
}

func (t *Transport) resolveConfig(ctx context.Context, req *transport.Request) (*gossh.ClientConfig, error) {
	if t.opts.ClientConfig != nil {
		return t.opts.ClientConfig(ctx, req)
	}

	// Default: use SSH agent auth with username from URL.
	var username string
	if req.URL.User != nil {
		username = req.URL.User.Username()
	}
	return DefaultAuthBuilder(username)
}

func (t *Transport) dial(ctx context.Context, network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
	// Honor timeout from ssh.ClientConfig.
	var cancel context.CancelFunc
	if config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	var conn net.Conn
	var err error

	switch {
	case t.opts.DialProxy != nil:
		dialFn := t.opts.DialContext
		if dialFn == nil {
			dialFn = (&net.Dialer{}).DialContext
		}
		conn, err = t.opts.DialProxy(dialFn)(ctx, network, addr)
	case t.opts.DialContext != nil:
		conn, err = t.opts.DialContext(ctx, network, addr)
	default:
		conn, err = (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	if err != nil {
		return nil, err
	}

	c, chans, reqs, err := gossh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}
	return gossh.NewClient(c, chans, reqs), nil
}



func resolveHostWithPort(req *transport.Request) string {
	hostname := req.URL.Hostname()
	port := req.URL.Port()

	if DefaultSSHConfig != nil {
		if configHost := DefaultSSHConfig.Get(hostname, "Hostname"); configHost != "" {
			hostname = configHost
		}
		if port == "" {
			if configPort := DefaultSSHConfig.Get(req.URL.Hostname(), "Port"); configPort != "" {
				if _, err := strconv.Atoi(configPort); err == nil {
					port = configPort
				}
			}
		}
	}

	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}

	return net.JoinHostPort(hostname, port)
}

type sshConn struct {
	stdout    io.Reader
	stdin     io.WriteCloser
	session   *gossh.Session
	client    *gossh.Client
	stderrBuf atomic.Pointer[bytes.Buffer]
}

func (c *sshConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if err != nil {
		if stderrErr := c.stderr(); stderrErr != nil {
			return n, stderrErr
		}
	}
	return n, err
}

func (c *sshConn) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *sshConn) Close() error {
	_ = c.stdin.Close()
	_ = c.session.Close()
	err := c.client.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	if stderrErr := c.stderr(); stderrErr != nil {
		return stderrErr
	}
	return err
}

func (c *sshConn) stderr() error {
	buf := c.stderrBuf.Load()
	if buf == nil {
		return nil
	}
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return nil
	}
	return transport.NewRemoteError(s)
}

func buildCommand(req *transport.Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s '%s'", req.Command, req.URL.Path)
	for _, arg := range req.Args {
		fmt.Fprintf(&b, " '%s'", arg)
	}
	return b.String()
}
