// Package transport implements the redesigned transport API for go-git.
//
// The API separates transport capabilities from Git protocol adapters:
//
//   - Conn: a transport connection with independent read, write, and close
//   - Connector: transports that can open raw full-duplex connections
//   - Transport: transports that speak the Git pack protocol (Handshake)
//   - Session: a connected Git protocol session (refs, fetch, push)
//
// Stream transports (SSH, Git TCP, file) implement both Connector and
// Transport. HTTP implements only Transport, speaking the smart HTTP
// protocol; the dumb HTTP protocol is not supported.
package transport
