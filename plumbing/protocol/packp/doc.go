// Package packp implements encoding and decoding of the messages of the
// Git pack protocol: reference advertisements, upload and update requests,
// negotiation responses, shallow updates, status reports, and the commands
// of protocol version 2.
//
// The wire formats are specified in gitprotocol-common(5),
// gitprotocol-pack(5), gitprotocol-capabilities(5) and gitprotocol-v2(5):
// https://git-scm.com/docs/gitprotocol-pack
//
// To see what Git itself sends and receives for an operation, run it with
// packet tracing enabled:
//
//	GIT_TRACE_PACKET=true git ls-remote https://github.com/go-git/go-git
package packp
