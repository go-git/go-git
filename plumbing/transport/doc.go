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
// protocol. The dumb HTTP protocol is not supported.
//
// # Using a transport
//
// Most callers do not use a Transport directly. The client package resolves
// a URL scheme to a Transport, and a custom Transport is registered there
// with client.WithTransport. A caller that holds a Transport calls
// Transport.Handshake with a Request naming the service (UploadPackService
// for fetch, ReceivePackService for push), then uses the returned Session:
//
//  1. Session.GetRemoteRefs lists the remote references.
//  2. Session.Fetch or Session.Push runs one pack exchange.
//  3. Session.Close releases the connection.
//
// A Session that negotiated Protocol v2 also implements Commander, and a
// Session that supports git-upload-archive implements Archiver. Callers
// type-assert for both.
//
// # Implementing a transport
//
// A new transport implements Transport, and Connector when it can open a
// raw full-duplex channel to the remote service. The package exports the
// protocol helpers the built-in transports use, so a custom transport only
// moves bytes and never encodes the Git protocol itself.
//
// A stream transport, one that runs the remote service and talks to its
// stdin and stdout (as SSH, Git TCP and file do), needs two steps:
//
//  1. Connect opens a Conn to the command named in Request.Command for the
//     repository in Request.URL.Path. When Request.Protocol is not V0, the
//     transport passes GitProtocolEnv(Request.Protocol) to the remote, as
//     the GIT_PROTOCOL environment variable or the transport's equivalent.
//  2. Handshake calls Connect and hands the Conn to NewStreamSession, which
//     reads the version and the capability or reference advertisement and
//     returns a Session. StreamSession implements Session, Commander and
//     Archiver, and owns the Conn from then on.
//
// The Conn contract is what such a transport has to get right. Reader
// returns the remote's output. Writer returns its input, and closing the
// Writer is a half-close that must not close the Reader. Conn.Close
// releases everything, and StreamSession calls it once, from
// Session.Close or when a context passed to it is done.
//
// A request/response transport, one that cannot keep a full-duplex channel
// open for the whole exchange (as smart HTTP), implements Session itself
// and builds it from the same helpers:
//
//  1. DiscoverVersion peeks at the first pkt-line of the advertisement to
//     pick the protocol version, and packp.AdvRefs or packp.CapabilityAdv
//     decodes the rest.
//  2. Session.Fetch calls NegotiatePack with statelessRPC set to true. Each
//     time NegotiatePack closes the writer, the transport sends the bytes
//     written so far as one request and serves the response on the reader.
//     NegotiatePack returns the shallow update the server sent, and the
//     reader is then left at the start of the pack response. It returns
//     ErrNoChange when the remote has nothing the storer lacks.
//  3. Session.Fetch then calls FetchPack with the same capabilities, that
//     shallow update and the pack response. FetchPack stores the pack and
//     updates the shallow list.
//  4. Session.Push calls SendPack. It writes the update commands and the
//     pack to the writer, closes it, and reads the report-status response
//     from the reader.
//
// A stream session calls the same three functions with statelessRPC set to
// false, so they are not tied to request/response transports. For Protocol
// v2, ReconcileObjectFormatV2 aligns the storer's object format with the
// server before any pack is requested. The Protocol v2 fetch exchange
// itself is only available through StreamSession and the HTTP transport,
// so a request/response transport that does not wrap a stream speaks
// Protocol v0 or v1. Archive speaks the git-upload-archive client protocol
// over any writer and reader pair.
//
// # Serving repositories
//
// UploadPack, ReceivePack and UploadArchive run the server side of each
// service over a reader and writer pair. AdvertiseRefs and
// AdvertiseCapabilities write the advertisement on their own, for
// transports such as smart HTTP that serve it as a separate request.
// A Loader resolves the URL of an incoming request to a storage.Storer.
// FilesystemLoader serves repositories from a billy.Filesystem, and
// MapLoader serves a fixed set of storers. The backend package combines
// these into a server for the HTTP and Git TCP protocols.
package transport
