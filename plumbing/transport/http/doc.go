// Package http implements the HTTP and HTTPS transport for the transport API.
//
// # Credentials
//
// A credential belongs to an origin: a scheme, a host and a port. The
// transport asks for one through Options.Credentials, a CredentialsFunc, which
// is given a CredentialRequest naming the origin a request is about to be made
// to and returns a Credential — an Authorizer that authenticates each outgoing
// request by mutating it.
//
// A credential configured for one origin is not sent to another; the single
// exception, an http origin upgrading to https on the same host, is described
// on ForOrigin. When a redirect leaves the origin a credential was issued for,
// the transport withholds it for the rest of the chain and asks
// Options.Credentials again, for the origin the redirect reached, so a caller
// that holds a credential there can supply it.
//
// ForOrigin builds a CredentialsFunc for an origin known up front and
// ForRepositoryOrigin one for the origin of whatever repository the caller
// names; Chain composes several sources into one.
package http
