package transport

import (
	"context"
	"io"
)

// Conn represents an open transport connection with independent read,
// write, and close operations.
//
// Writer().Close() signals the end of writing (half-close) without closing
// the read side. Close() releases all resources.
//
// For stream transports (SSH, Git TCP, file), Reader() and Writer() refer
// to the remote command's stdout and stdin respectively.
//
// For HTTP, Conn models a single request/response exchange where Writer()
// buffers the request body and closing it triggers the HTTP round-trip.
type Conn interface {
	io.Closer
	Reader() io.Reader
	Writer() io.WriteCloser
}

// Connector is implemented by transports that can open a raw full-duplex
// connection. SSH, Git TCP, and file transports implement this.
// HTTP does not.
//
// Use Connect for non-pack protocols like git-upload-archive,
// git-lfs-authenticate, git-lfs-transfer, or custom commands.
type Connector interface {
	Connect(context.Context, *Request) (Conn, error)
}
