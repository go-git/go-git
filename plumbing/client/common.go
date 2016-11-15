// Package clients includes the implementation for different transport protocols
//
// go-git needs the packfile and the refs of the repo. The
// `NewGitUploadPackService` function returns an object that allows to
// download them.
//
// go-git supports HTTP and SSH (see `Protocols`) for downloading the packfile
// and the refs, but you can also install your own protocols (see
// `InstallProtocol` below).
//
// Each protocol has its own implementation of
// `NewGitUploadPackService`, but you should generally not use them
// directly, use this package's `NewGitUploadPackService` instead.
package clients

import (
	"fmt"

	"gopkg.in/src-d/go-git.v4/plumbing/client/common"
	"gopkg.in/src-d/go-git.v4/plumbing/client/http"
	"gopkg.in/src-d/go-git.v4/plumbing/client/ssh"
)

// Protocols are the protocols supported by default.
var Protocols = map[string]common.GitUploadPackServiceFactory{
	"http":  http.NewGitUploadPackService,
	"https": http.NewGitUploadPackService,
	"ssh":   ssh.NewGitUploadPackService,
}

// InstallProtocol adds or modifies an existing protocol.
func InstallProtocol(scheme string, f common.GitUploadPackServiceFactory) {
	Protocols[scheme] = f
}

// NewGitUploadPackService returns the appropriate upload pack service
// among of the set of known protocols: HTTP, SSH. See `InstallProtocol`
// to add or modify protocols.
func NewGitUploadPackService(endpoint common.Endpoint) (common.GitUploadPackService, error) {
	f, ok := Protocols[endpoint.Scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported scheme %q", endpoint.Scheme)
	}

	return f(endpoint), nil
}
