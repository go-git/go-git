package client

import (
	"fmt"

	"srcd.works/go-git.v4/plumbing/transport"
	"srcd.works/go-git.v4/plumbing/transport/file"
	"srcd.works/go-git.v4/plumbing/transport/git"
	"srcd.works/go-git.v4/plumbing/transport/http"
	"srcd.works/go-git.v4/plumbing/transport/ssh"
)

// Protocols are the protocols supported by default.
var Protocols = map[string]transport.Transport{
	"http":  http.DefaultClient,
	"https": http.DefaultClient,
	"ssh":   ssh.DefaultClient,
	"git":   git.DefaultClient,
	"file":  file.DefaultClient,
}

// InstallProtocol adds or modifies an existing protocol.
func InstallProtocol(scheme string, c transport.Transport) {
	Protocols[scheme] = c
}

// NewClient returns the appropriate client among of the set of known protocols:
// http://, https://, ssh:// and file://.
// See `InstallProtocol` to add or modify protocols.
func NewClient(endpoint transport.Endpoint) (transport.Transport, error) {
	f, ok := Protocols[endpoint.Scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported scheme %q", endpoint.Scheme)
	}

	return f, nil
}
