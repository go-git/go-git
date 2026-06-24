package transport

import "errors"

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
)

// Negotiation errors.
var (
	ErrFilterNotSupported  = errors.New("server does not support filters")
	ErrShallowNotSupported = errors.New("server does not support shallow clients")
)
