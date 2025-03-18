// Package transport includes the implementation for different transport
// protocols.
//
// `Client` can be used to fetch and send packfiles to a git server.
// The `client` package provides higher level functions to instantiate the
// appropriate `Client` based on the repository URL.
//
// go-git supports HTTP and SSH (see `Protocols`), but you can also install
// your own protocols (see the `client` package).
//
// Each protocol has its own implementation of `Client`, but you should
// generally not use them directly, use `client.NewClient` instead.
package transport

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	giturl "github.com/go-git/go-git/v6/internal/url"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage"
)

var (
	ErrRepositoryNotFound     = errors.New("repository not found")
	ErrEmptyRemoteRepository  = errors.New("remote repository is empty")
	ErrNoChange               = errors.New("no change")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrAuthorizationFailed    = errors.New("authorization failed")
	ErrEmptyUploadPackRequest = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod      = errors.New("invalid auth method")
	ErrAlreadyConnected       = errors.New("session already established")
)

// Transport can initiate git-upload-pack and git-receive-pack processes.
// It is implemented both by the client and the server, making this a RPC.
type Transport interface {
	// NewSession returns a new session for an endpoint.
	NewSession(storage.Storer, *Endpoint, AuthMethod) (Session, error)

	// SupportedProtocols returns a list of supported Git protocol versions by
	// the transport client.
	SupportedProtocols() []protocol.Version
}

type AuthMethod interface {
	fmt.Stringer
	Name() string
}

// Endpoint represents a Git URL in any supported protocol.
type Endpoint struct {
	// Protocol is the protocol of the endpoint (e.g. git, https, file).
	Protocol string
	// User is the user.
	User string
	// Password is the password.
	Password string
	// Host is the host.
	Host string
	// Port is the port to connect, if 0 the default port for the given protocol
	// will be used.
	Port int
	// Path is the repository path.
	Path string
	// InsecureSkipTLS skips ssl verify if protocol is https
	InsecureSkipTLS bool
	// CaBundle specify additional ca bundle with system cert pool
	CaBundle []byte
	// Proxy provides info required for connecting to a proxy.
	Proxy ProxyOptions
}

type ProxyOptions struct {
	URL      string
	Username string
	Password string
}

func (o *ProxyOptions) Validate() error {
	if o.URL != "" {
		_, err := url.Parse(o.URL)
		return err
	}
	return nil
}

func (o *ProxyOptions) FullURL() (*url.URL, error) {
	proxyURL, err := url.Parse(o.URL)
	if err != nil {
		return nil, err
	}
	if o.Username != "" {
		if o.Password != "" {
			proxyURL.User = url.UserPassword(o.Username, o.Password)
		} else {
			proxyURL.User = url.User(o.Username)
		}
	}
	return proxyURL, nil
}

var defaultPorts = map[string]int{
	"http":  80,
	"https": 443,
	"git":   9418,
	"ssh":   22,
}

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)

// String returns a string representation of the Git URL.
func (u *Endpoint) String() string {
	var buf bytes.Buffer
	if u.Protocol != "" {
		buf.WriteString(u.Protocol)
		buf.WriteByte(':')
	}

	if u.Protocol != "" || u.Host != "" || u.User != "" || u.Password != "" {
		buf.WriteString("//")

		if u.User != "" || u.Password != "" {
			buf.WriteString(url.PathEscape(u.User))
			if u.Password != "" {
				buf.WriteByte(':')
				buf.WriteString(url.PathEscape(u.Password))
			}

			buf.WriteByte('@')
		}

		if u.Host != "" {
			buf.WriteString(u.Host)

			if u.Port != 0 {
				port, ok := defaultPorts[strings.ToLower(u.Protocol)]
				if !ok || ok && port != u.Port {
					fmt.Fprintf(&buf, ":%d", u.Port)
				}
			}
		}
	}

	if u.Path != "" && u.Path[0] != '/' && u.Host != "" {
		buf.WriteByte('/')
	}

	buf.WriteString(u.Path)
	return buf.String()
}

func NewEndpoint(endpoint string) (*Endpoint, error) {
	if e, ok := parseSCPLike(endpoint); ok {
		return e, nil
	}

	if e, ok := parseFile(endpoint); ok {
		return e, nil
	}

	return parseURL(endpoint)
}

func parseURL(endpoint string) (*Endpoint, error) {
	if strings.HasPrefix(endpoint, "file://") {
		endpoint = strings.TrimPrefix(endpoint, "file://")

		// When triple / is used, the path in Windows may end up having an
		// additional / resulting in "/C:/Dir".
		if runtime.GOOS == "windows" &&
			fileIssueWindows.MatchString(endpoint) {
			endpoint = endpoint[1:]
		}
		return &Endpoint{
			Protocol: "file",
			Path:     endpoint,
		}, nil
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	if !u.IsAbs() {
		return nil, plumbing.NewPermanentError(fmt.Errorf(
			"invalid endpoint: %s", endpoint,
		))
	}

	var user, pass string
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}

	host := u.Hostname()
	if strings.Contains(host, ":") {
		// IPv6 address
		host = "[" + host + "]"
	}

	return &Endpoint{
		Protocol: u.Scheme,
		User:     user,
		Password: pass,
		Host:     host,
		Port:     getPort(u),
		Path:     getPath(u),
	}, nil
}

func getPort(u *url.URL) int {
	p := u.Port()
	if p == "" {
		return 0
	}

	i, err := strconv.Atoi(p)
	if err != nil {
		return 0
	}

	return i
}

func getPath(u *url.URL) string {
	var res string = u.Path
	if u.RawQuery != "" {
		res += "?" + u.RawQuery
	}

	if u.Fragment != "" {
		res += "#" + u.Fragment
	}

	return res
}

func parseSCPLike(endpoint string) (*Endpoint, bool) {
	if giturl.MatchesScheme(endpoint) || !giturl.MatchesScpLike(endpoint) {
		return nil, false
	}

	user, host, portStr, path := giturl.FindScpLikeComponents(endpoint)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 22
	}

	return &Endpoint{
		Protocol: "ssh",
		User:     user,
		Host:     host,
		Port:     port,
		Path:     path,
	}, true
}

func parseFile(endpoint string) (*Endpoint, bool) {
	if giturl.MatchesScheme(endpoint) {
		return nil, false
	}

	path := endpoint
	return &Endpoint{
		Protocol: "file",
		Path:     path,
	}, true
}
