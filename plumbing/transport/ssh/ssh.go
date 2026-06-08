// Package ssh implements the SSH transport for the new transport API.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/kevinburke/ssh_config"
	gossh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// DefaultPort is the default port for the SSH protocol.
const DefaultPort = 22

// DefaultUsername is the default username for SSH connections.
const DefaultUsername = "git"

// Options configures the SSH transport.
type Options struct {
	// ClientConfig provides SSH client configuration for each request.
	// If nil, SSH agent authentication is used with the username from the
	// URL (falling back to DefaultUsername).
	ClientConfig func(context.Context, *transport.Request) (*gossh.ClientConfig, error)

	// DialContext is the function used to establish TCP connections.
	// If nil, a default net.Dialer is used.
	DialContext transport.DialContextFunc

	// DialProxy wraps DialContext to route connections through a proxy.
	// If nil, connections are made directly.
	DialProxy func(transport.DialContextFunc) transport.DialContextFunc

	// UserSettings provides an SSH configuration (Hostname, Port overrides
	// from ~/.ssh/config). If nil, [ssh_config.DefaultUserSettings] is used.
	UserSettings func(context.Context, *transport.Request) (*ssh_config.UserSettings, error)
}

// Transport implements the ssh:// transport protocol.
type Transport struct {
	opts Options
}

// NewTransport creates an SSH transport with the given options.
func NewTransport(opts Options) *Transport {
	return &Transport{opts: opts}
}

// Connect implements transport.Connector.
func (t *Transport) Connect(ctx context.Context, req *transport.Request) (transport.Conn, error) {
	conn, err := t.connect(ctx, req)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (t *Transport) connect(ctx context.Context, req *transport.Request) (*sshConn, error) {
	config, err := t.resolveConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	hostWithPort, err := t.resolveHostWithPort(ctx, req)
	if err != nil {
		return nil, err
	}

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

	trace.SSH.Printf("ssh: host key algorithms %s", strings.Join(config.HostKeyAlgorithms, ", "))

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

	username := DefaultUsername
	if req.URL.User != nil {
		if u := req.URL.User.Username(); u != "" {
			username = u
		}
	}

	trace.SSH.Printf("ssh: Using default auth builder (user: %s)", username)

	auth, err := NewSSHAgentAuth(username)
	if err != nil {
		return nil, err
	}
	return auth.ClientConfig(ctx, req)
}

func (t *Transport) dial(ctx context.Context, network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
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
		trace.SSH.Printf("ssh: using proxyURL for connection")
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

func (t *Transport) userSettings(ctx context.Context, req *transport.Request) (*ssh_config.UserSettings, error) {
	if t.opts.UserSettings != nil {
		return t.opts.UserSettings(ctx, req)
	}
	return ssh_config.DefaultUserSettings, nil
}

func (t *Transport) resolveHostWithPort(ctx context.Context, req *transport.Request) (string, error) {
	hostname := req.URL.Hostname()
	port := req.URL.Port()

	cfg, err := t.userSettings(ctx, req)
	if err != nil {
		return "", err
	}
	if configHost := cfg.Get(hostname, "Hostname"); configHost != "" {
		hostname = configHost
	}
	if port == "" {
		if configPort := cfg.Get(req.URL.Hostname(), "Port"); configPort != "" {
			if _, err := strconv.Atoi(configPort); err == nil {
				port = configPort
			}
		}
	}

	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}

	return net.JoinHostPort(hostname, port), nil
}

type sshConn struct {
	stdout    io.Reader
	stdin     io.WriteCloser
	session   *gossh.Session
	client    *gossh.Client
	stderrBuf atomic.Pointer[bytes.Buffer]
}

var _ transport.Conn = (*sshConn)(nil)

func (c *sshConn) Reader() io.Reader      { return c.stdout }
func (c *sshConn) Writer() io.WriteCloser { return c.stdin }

func (c *sshConn) Close() error {
	_ = c.session.Close()
	err := c.client.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	return err
}

// Stderr returns the stderr stream from the remote process. StreamSession
// checks for this after Push/Fetch to surface remote errors at the
// operation site rather than at Close time.
func (c *sshConn) Stderr() io.Reader {
	buf := c.stderrBuf.Load()
	if buf == nil {
		return nil
	}
	return buf
}

func buildCommand(req *transport.Request) string {
	var b strings.Builder
	b.WriteString(req.Command)
	b.WriteByte(' ')
	writeShellQuote(&b, req.URL.Path)
	for _, arg := range req.Args {
		b.WriteByte(' ')
		writeShellQuote(&b, arg)
	}
	return b.String()
}

// writeShellQuote writes s to b, wrapped in single quotes with
// embedded single quotes and exclamation marks escaped using the
// POSIX close-escape-reopen idiom:
//
//	' becomes '\''
//	! becomes '\!'
//
// It is a direct port of canonical Git's sq_quote_buf (quote.c).
// The bang escape keeps the result safe when re-evaluated under
// csh-derived shells that perform history expansion. The output is
// safe to pass as a single argument through any POSIX shell and
// round-trips through git-shell's sq_dequote_to_argv.
func writeShellQuote(b *strings.Builder, s string) {
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '!' {
			b.WriteString(`'\`)
			b.WriteByte(c)
			b.WriteByte('\'')
			continue
		}
		b.WriteByte(c)
	}
	b.WriteByte('\'')
}
