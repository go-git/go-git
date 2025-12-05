// Package ssh implements the SSH transport protocol.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/trace"
)

func init() {
	transport.Register("ssh", DefaultTransport)
}

// DefaultTransport is the default SSH client.
var DefaultTransport = NewTransport(nil)

// DefaultSSHConfig is the reader used to access parameters stored in the
// system's ssh_config files. If nil all the ssh_config are ignored.
var DefaultSSHConfig sshConfig = ssh_config.DefaultUserSettings

type sshConfig interface {
	Get(alias, key string) string
}

// NewTransport creates a new SSH client with an optional *ssh.ClientConfig.
func NewTransport(config *ssh.ClientConfig) transport.Transport {
	return transport.NewPackTransport(&runner{config: config})
}

// DefaultAuthBuilder is the function used to create a default AuthMethod, when
// the user doesn't provide any.
var DefaultAuthBuilder = func(user string) (AuthMethod, error) {
	trace.SSH.Printf("ssh: Using default auth builder (user: %s)", user)
	return NewSSHAgentAuth(user)
}

// DefaultPort is the default port for the SSH protocol.
const DefaultPort = 22

type runner struct {
	config *ssh.ClientConfig
}

func (r *runner) Command(ctx context.Context, cmd string, ep *transport.Endpoint, auth transport.AuthMethod, params ...string) (transport.Command, error) {
	c := &command{command: cmd, endpoint: ep, config: r.config}
	if auth != nil {
		if err := c.setAuth(auth); err != nil {
			return nil, err
		}
	}

	gitProtocol := strings.Join(params, ":")
	if err := c.connect(ctx); err != nil {
		return nil, err
	}

	if gitProtocol != "" {
		if err := c.Setenv("GIT_PROTOCOL", gitProtocol); err != nil {
			return nil, err
		}
	}

	return c, nil
}

type command struct {
	*ssh.Session
	connected bool
	command   string
	endpoint  *transport.Endpoint
	client    *ssh.Client
	auth      AuthMethod
	config    *ssh.ClientConfig
}

func (c *command) setAuth(auth transport.AuthMethod) error {
	a, ok := auth.(AuthMethod)
	if !ok {
		return transport.ErrInvalidAuthMethod
	}

	c.auth = a
	return nil
}

func (c *command) Start() error {
	cmd := endpointToCommand(c.command, c.endpoint)
	return c.Session.Start(cmd)
}

// Close closes the SSH session and connection.
func (c *command) Close() error {
	if !c.connected {
		return nil
	}

	c.connected = false

	// XXX: If did read the full packfile, then the session might be already
	//     closed.
	_ = c.Session.Close()
	err := c.client.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}

// connect connects to the SSH server, unless a AuthMethod was set with
// SetAuth method, by default uses an auth method based on PublicKeysCallback,
// it connects to a SSH agent, using the address stored in the SSH_AUTH_SOCK
// environment var.
func (c *command) connect(ctx context.Context) error {
	if c.connected {
		return transport.ErrAlreadyConnected
	}

	if c.auth == nil {
		if err := c.setAuthFromEndpoint(); err != nil {
			return err
		}
	}

	var err error
	config, err := c.auth.ClientConfig()
	if err != nil {
		return err
	}
	hostWithPort := c.getHostWithPort()
	if config.HostKeyCallback == nil {
		db, err := newKnownHostsDb()
		if err != nil {
			return err
		}

		config.HostKeyCallback = db.HostKeyCallback()
		config.HostKeyAlgorithms = db.HostKeyAlgorithms(hostWithPort)
	} else if len(config.HostKeyAlgorithms) == 0 {
		// Set the HostKeyAlgorithms based on HostKeyCallback.
		// For background see https://github.com/go-git/go-git/issues/411 as well as
		// https://github.com/golang/go/issues/29286 for root cause.
		db, err := newKnownHostsDb()
		if err != nil {
			return err
		}

		// Note that the knownhost database is used, as it provides additional functionality
		// to handle ssh cert-authorities.
		config.HostKeyAlgorithms = db.HostKeyAlgorithms(hostWithPort)
	}

	trace.SSH.Printf("ssh: host key algorithms %s", config.HostKeyAlgorithms)

	overrideConfig(c.config, config)

	c.client, err = dial(ctx, "tcp", hostWithPort, c.endpoint.Proxy, config)
	if err != nil {
		return err
	}

	c.Session, err = c.client.NewSession()
	if err != nil {
		_ = c.client.Close()
		return err
	}

	c.connected = true
	return nil
}

func dial(ctx context.Context, network, addr string, proxyOpts transport.ProxyOptions, config *ssh.ClientConfig) (*ssh.Client, error) {
	var cancel context.CancelFunc
	if config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	var conn net.Conn
	var dialErr error

	if proxyOpts.URL != "" {
		proxyURL, err := proxyOpts.FullURL()
		if err != nil {
			return nil, err
		}

		trace.SSH.Printf("ssh: using proxyURL=%s", proxyURL)
		dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, err
		}

		// Try to use a ContextDialer, but fall back to a Dialer if that goes south.
		ctxDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("expected ssh proxy dialer to be of type %s; got %s",
				reflect.TypeOf(ctxDialer), reflect.TypeOf(dialer))
		}
		conn, dialErr = ctxDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, dialErr = proxy.Dial(ctx, network, addr)
	}
	if dialErr != nil {
		return nil, dialErr
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func (c *command) getHostWithPort() string {
	if addr, found := c.doGetHostWithPortFromSSHConfig(); found {
		return addr
	}

	host := c.endpoint.Hostname()
	port := c.endpoint.Port()
	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}

	return net.JoinHostPort(host, port)
}

func (c *command) doGetHostWithPortFromSSHConfig() (addr string, found bool) {
	if DefaultSSHConfig == nil {
		return addr, found
	}

	hostname := c.endpoint.Hostname()
	port := c.endpoint.Port()

	configHost := DefaultSSHConfig.Get(c.endpoint.Hostname(), "Hostname")
	if configHost != "" {
		hostname = configHost
		found = true
	}

	if !found {
		return addr, found
	}

	configPort := DefaultSSHConfig.Get(c.endpoint.Hostname(), "Port")
	if configPort != "" {
		if _, err := strconv.Atoi(configPort); err == nil {
			port = configPort
		}
	}

	addr = net.JoinHostPort(hostname, port)
	return addr, found
}

func (c *command) setAuthFromEndpoint() error {
	var err error
	var username string
	if c.endpoint.User != nil {
		username = c.endpoint.User.Username()
	}
	c.auth, err = DefaultAuthBuilder(username)
	return err
}

func endpointToCommand(cmd string, ep *transport.Endpoint) string {
	return fmt.Sprintf("%s '%s'", cmd, ep.Path)
}

func overrideConfig(overrides, c *ssh.ClientConfig) {
	if overrides == nil {
		return
	}

	t := reflect.TypeFor[ssh.ClientConfig]()
	vc := reflect.ValueOf(c).Elem()
	vo := reflect.ValueOf(overrides).Elem()

	for i := range t.NumField() {
		f := t.Field(i)
		vcf := vc.FieldByName(f.Name)
		vof := vo.FieldByName(f.Name)
		vcf.Set(vof)
	}

	*c = vc.Interface().(ssh.ClientConfig)
}
