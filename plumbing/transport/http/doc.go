// Package http implements the HTTP and HTTPS transport for the transport API.
//
// # Credentials
//
// A credential belongs to an origin: a scheme, a host and a port.
// Options.Credentials supplies one for the origin each request is about to be
// made to. ForRepositoryOrigin builds a source for the origin of whatever
// repository the caller names, ForOrigin one for an origin known up front, and
// Chain composes several sources into one.
//
// A credential configured for one origin is not sent to another; the sole
// exception, the http-to-https upgrade, is described on ForOrigin. When a
// redirect leaves the origin a credential was issued for, the transport
// withholds it and asks Options.Credentials again for the origin the redirect
// reached, so an authentication failure there carries
// transport.CredentialsDroppedError rather than a leaked secret.
package http
