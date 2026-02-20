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
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime"
	"strings"

	giturl "github.com/go-git/go-git/v6/internal/url"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage"
)

// Transport errors.
var (
	ErrRepositoryNotFound     = errors.New("repository not found")
	ErrEmptyRemoteRepository  = errors.New("remote repository is empty")
	ErrNoChange               = errors.New("no change")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrAuthorizationFailed    = errors.New("authorization failed")
	ErrEmptyUploadPackRequest = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod      = errors.New("invalid auth method")
	ErrAlreadyConnected       = errors.New("session already established")
	ErrInvalidRequest         = errors.New("invalid request")
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

// AuthMethod defines the interface for authentication.
type AuthMethod interface {
	fmt.Stringer
	Name() string
}

// Endpoint represents a Git URL in any supported protocol.
type Endpoint struct {
	url.URL

	// InsecureSkipTLS skips ssl verify if protocol is https
	InsecureSkipTLS bool
	// CaBundle specify additional ca bundle with system cert pool
	CaBundle []byte
	// Proxy provides info required for connecting to a proxy.
	Proxy ProxyOptions
}

// ProxyOptions provides configuration for proxy connections.
type ProxyOptions struct {
	URL      string
	Username string
	Password string
}

// Validate validates the proxy options.
func (o *ProxyOptions) Validate() error {
	if o.URL != "" {
		_, err := url.Parse(o.URL)
		return err
	}
	return nil
}

// FullURL returns the full proxy URL including credentials.
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

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)
var windowsDrive = regexp.MustCompile(`^[A-Za-z]:(/|\\)`)

// NewEndpoint parses an endpoint string and returns an Endpoint.
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
	if after, ok := strings.CutPrefix(endpoint, "file://"); ok {
		endpoint = after

		// When triple / is used, the path in Windows may end up having an
		// additional / resulting in "/C:/Dir".
		if runtime.GOOS == "windows" &&
			fileIssueWindows.MatchString(endpoint) {
			endpoint = endpoint[1:]
		}
		return &Endpoint{
			URL: url.URL{
				Scheme: "file",
				Path:   endpoint,
			},
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

	return &Endpoint{
		URL: *u,
	}, nil
}

func parseSCPLike(endpoint string) (*Endpoint, bool) {
	// On Windows, a path like "C:/..." or "C:\\..." should be treated
	// as a local file path, not an SCP-like SSH URL. Avoid matching those
	// as SCP-like endpoints.
	if runtime.GOOS == "windows" && windowsDrive.MatchString(endpoint) {
		return nil, false
	}
	if giturl.MatchesScheme(endpoint) || !giturl.MatchesScpLike(endpoint) {
		return nil, false
	}

	user, host, port, path := giturl.FindScpLikeComponents(endpoint)
	if port != "" {
		host = net.JoinHostPort(host, port)
	}

	return &Endpoint{
		URL: url.URL{
			Scheme: "ssh",
			User:   url.User(user),
			Host:   host,
			Path:   path,
		},
	}, true
}

func parseFile(endpoint string) (*Endpoint, bool) {
	if giturl.MatchesScheme(endpoint) {
		return nil, false
	}

	path := endpoint
	return &Endpoint{
		URL: url.URL{
			Scheme: "file",
			Path:   path,
		},
	}, true
}
