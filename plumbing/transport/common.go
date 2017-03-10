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
	"io"
	"net/url"
	"regexp"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
)

var (
	ErrRepositoryNotFound     = errors.New("repository not found")
	ErrEmptyRemoteRepository  = errors.New("remote repository is empty")
	ErrAuthorizationRequired  = errors.New("authorization required")
	ErrEmptyUploadPackRequest = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod      = errors.New("invalid auth method")
	ErrAlreadyConnected       = errors.New("session already established")
)

const (
	UploadPackServiceName  = "git-upload-pack"
	ReceivePackServiceName = "git-receive-pack"
)

// Transport can initiate git-upload-pack and git-receive-pack processes.
// It is implemented both by the client and the server, making this a RPC.
type Transport interface {
	// NewUploadPackSession starts a git-upload-pack session for an endpoint.
	NewUploadPackSession(Endpoint, AuthMethod) (UploadPackSession, error)
	// NewReceivePackSession starts a git-receive-pack session for an endpoint.
	NewReceivePackSession(Endpoint, AuthMethod) (ReceivePackSession, error)
}

type Session interface {
	// AdvertisedReferences retrieves the advertised references for a
	// repository.
	// If the repository does not exist, returns ErrRepositoryNotFound.
	// If the repository exists, but is empty, returns ErrEmptyRemoteRepository.
	AdvertisedReferences() (*packp.AdvRefs, error)
	io.Closer
}

type AuthMethod interface {
	fmt.Stringer
	Name() string
}

// UploadPackSession represents a git-upload-pack session.
// A git-upload-pack session has two steps: reference discovery
// (AdvertisedReferences) and uploading pack (UploadPack).
type UploadPackSession interface {
	Session
	// UploadPack takes a git-upload-pack request and returns a response,
	// including a packfile. Don't be confused by terminology, the client
	// side of a git-upload-pack is called git-fetch-pack, although here
	// the same interface is used to make it RPC-like.
	UploadPack(*packp.UploadPackRequest) (*packp.UploadPackResponse, error)
}

// ReceivePackSession represents a git-receive-pack session.
// A git-receive-pack session has two steps: reference discovery
// (AdvertisedReferences) and receiving pack (ReceivePack).
// In that order.
type ReceivePackSession interface {
	Session
	// ReceivePack sends an update references request and a packfile
	// reader and returns a ReportStatus and error. Don't be confused by
	// terminology, the client side of a git-receive-pack is called
	// git-send-pack, although here the same interface is used to make it
	// RPC-like.
	ReceivePack(*packp.ReferenceUpdateRequest) (*packp.ReportStatus, error)
}

type Endpoint url.URL

var (
	isSchemeRegExp   = regexp.MustCompile("^[^:]+://")
	scpLikeUrlRegExp = regexp.MustCompile("^(?P<user>[^@]+@)?(?P<host>[^:]+):/?(?P<path>.+)$")
)

func NewEndpoint(endpoint string) (Endpoint, error) {
	endpoint = transformSCPLikeIfNeeded(endpoint)

	u, err := url.Parse(endpoint)
	if err != nil {
		return Endpoint{}, plumbing.NewPermanentError(err)
	}

	if !u.IsAbs() {
		return Endpoint{}, plumbing.NewPermanentError(fmt.Errorf(
			"invalid endpoint: %s", endpoint,
		))
	}

	return Endpoint(*u), nil
}

func (e *Endpoint) String() string {
	u := url.URL(*e)
	return u.String()
}

func transformSCPLikeIfNeeded(endpoint string) string {
	if !isSchemeRegExp.MatchString(endpoint) && scpLikeUrlRegExp.MatchString(endpoint) {
		m := scpLikeUrlRegExp.FindStringSubmatch(endpoint)
		return fmt.Sprintf("ssh://%s%s/%s", m[1], m[2], m[3])
	}

	return endpoint
}

// UnsupportedCapabilities are the capabilities not supported by any client
// implementation
var UnsupportedCapabilities = []capability.Capability{
	capability.MultiACK,
	capability.MultiACKDetailed,
	capability.ThinPack,
}

// FilterUnsupportedCapabilities it filter out all the UnsupportedCapabilities
// from a capability.List, the intended usage is on the client implementation
// to filter the capabilities from an AdvRefs message.
func FilterUnsupportedCapabilities(list *capability.List) {
	for _, c := range UnsupportedCapabilities {
		list.Delete(c)
	}
}
