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
)

// ErrPackJunk is returned by a fetch, through [FetchPack] or a v2
// [Session.Fetch], when the storage leaves pack data unread after the pack
// trailer. The pack is already stored by then. Git's outcome for such data
// depends on how it unpacks the pack, and the check is best effort: the
// fetch reports only junk the storage left unread, so small junk can go
// unnoticed. See [FetchPack].
var ErrPackJunk = internal.ErrPackJunk

// Transport capability and support errors.
var (
	ErrConnectUnsupported        = errors.New("transport does not support raw connections")
	ErrArchiveUnsupported        = errors.New("transport does not support archive")
	ErrCommandUnsupported        = errors.New("command is not supported by transport")
	ErrUnsupportedVersion        = errors.New("unsupported protocol version")
	ErrUnsupportedService        = errors.New("unsupported service")
	ErrInvalidResponse           = errors.New("invalid response")
	ErrPackedObjectsNotSupported = errors.New("packed objects not supported")
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
