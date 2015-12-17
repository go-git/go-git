// Go-git needs the packfile and the refs of the repo. The
// `NewGitUploadPackService` function returns an object that allows to
// download them.
//
// Go-git supports HTTP and SSH (see `KnownProtocols`) for downloading
// the packfile and the refs, but you can also install your own
// protocols (see `InstallProtocol` below).
//
// Each protocol has its own implementation of
// `NewGitUploadPackService`, but you should generally not use them
// directly, use this package's `NewGitUploadPackService` instead.
package clients

import (
	"fmt"
	"net/url"

	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/clients/http"
	"gopkg.in/src-d/go-git.v2/clients/ssh"
)

// ServiceFromURLFunc defines a service returning function for a given
// URL.
type ServiceFromURLFunc func(url string) common.GitUploadPackService

// DefaultProtocols are the protocols supported by default.
// Wrapping is needed because you can not cast a function that
// returns an implementation of an interface to a function that
// returns the interface.
var DefaultProtocols = map[string]ServiceFromURLFunc{
	"http":  func(s string) common.GitUploadPackService { return http.NewGitUploadPackService(s) },
	"https": func(s string) common.GitUploadPackService { return http.NewGitUploadPackService(s) },
	"ssh":   func(s string) common.GitUploadPackService { return ssh.NewGitUploadPackService(s) },
}

// KnownProtocols holds the current set of known protocols. Initially
// it gets its contents from `DefaultProtocols`. See `InstallProtocol`
// below to add or modify this variable.
var KnownProtocols = make(map[string]ServiceFromURLFunc, len(DefaultProtocols))

func init() {
	for k, v := range DefaultProtocols {
		KnownProtocols[k] = v
	}
}

// NewGitUploadPackService returns the appropiate upload pack service
// among of the set of known protocols: HTTP, SSH. See `InstallProtocol`
// to add or modify protocols.
func NewGitUploadPackService(repoURL string) (common.GitUploadPackService, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url %q", repoURL)
	}
	srvFn, ok := KnownProtocols[u.Scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	return srvFn(repoURL), nil
}

// InstallProtocol adds or modifies an existing protocol.
func InstallProtocol(scheme string, serviceFn ServiceFromURLFunc) {
	if serviceFn == nil {
		panic("nil service")
	}
	KnownProtocols[scheme] = serviceFn
}
