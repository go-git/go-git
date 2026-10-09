package transport

import (
	"errors"
	"fmt"
	"net/url"

	internal "github.com/go-git/go-git/v6/internal/transport"
)

// Transport errors.
var (
	ErrRepositoryNotFound     = errors.New("repository not found")
	ErrEmptyRemoteRepository  = errors.New("remote repository is empty")
	ErrNoChange               = internal.ErrNoChange
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrAuthorizationFailed    = errors.New("authorization failed")
	ErrEmptyUploadPackRequest = errors.New("empty git-upload-pack given")
	ErrInvalidAuthMethod      = errors.New("invalid auth method")
	ErrAlreadyConnected       = errors.New("session already established")
	ErrInvalidRequest         = errors.New("invalid request")
)

// Transport capability and support errors.
var (
	ErrConnectUnsupported        = errors.New("transport does not support raw connections")
	ErrArchiveUnsupported        = errors.New("transport does not support archive")
	ErrCommandUnsupported        = errors.New("command is not supported by transport")
	ErrProtocolUnsupported       = errors.New("protocol version is not supported")
	ErrUnsupportedVersion        = errors.New("unsupported protocol version")
	ErrUnsupportedService        = errors.New("unsupported service")
	ErrInvalidResponse           = errors.New("invalid response")
	ErrTimeoutExceeded           = errors.New("timeout exceeded")
	ErrPackedObjectsNotSupported = errors.New("packed objects not supported")

	// ErrUnsupportedObjectFormat is returned by SendPack when the server does not
	// support the local repository's object format.
	ErrUnsupportedObjectFormat = errors.New("the receiving end does not support this repository's hash algorithm")
)

// Negotiation errors.
var (
	ErrFilterNotSupported  = errors.New("server does not support filters")
	ErrShallowNotSupported = errors.New("server does not support shallow clients")
)

// CredentialsDroppedError reports that a redirect chain left the origin
// credentials were issued for, so they were not sent to the origin the chain
// ended at. Withholding is sticky: a chain that leaves the origin and returns
// has still left it.
//
// It never appears alone — a redirect target that serves the repository
// anonymously is a success — and only where a credential existed to withhold.
// It annotates ErrAuthorizationFailed as well as ErrAuthenticationRequired, so
// matching only on the latter misses the failures a 403 produces. The
// annotated error wraps two errors, so errors.Unwrap returns nil on it;
// errors.Is and errors.As are the way in.
//
//	var dropped *transport.CredentialsDroppedError
//	if errors.As(err, &dropped) {
//	        // dropped.To is the origin that needs a credential
//	}
type CredentialsDroppedError struct {
	// From is the repository origin the withheld credential belonged to. It is
	// an independent copy carrying scheme and host only.
	From *url.URL
	// To is the final origin that refused the unauthenticated request, an
	// independent copy carrying scheme and host only. It can equal From when a
	// chain leaves that origin and returns.
	To *url.URL
}

// Error implements the error interface. A nil From or To renders as "<nil>".
func (e *CredentialsDroppedError) Error() string {
	return fmt.Sprintf(
		"credentials for %s were not sent to %s because a redirect crossed an origin boundary",
		originString(e.From), originString(e.To),
	)
}

// originString renders u, or "<nil>" when u is nil. Passing a nil *url.URL to
// fmt directly would rely on fmt recovering the nil dereference in
// (*url.URL).String, and on Windows the hardware fault behind that can
// corrupt the heap (golang/go#81238).
func originString(u *url.URL) string {
	if u == nil {
		return "<nil>"
	}
	return u.String()
}
