package server

import (
	"io"

	"github.com/go-git/go-git/v6/internal/server/git"
	"github.com/go-git/go-git/v6/internal/server/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// GitServer represents a git transport server that can be started and stopped.
type GitServer interface {
	Start() (string, error)
	io.Closer
}

// All returns all available git server implementations backed by the given loader.
func All(l transport.Loader) []GitServer {
	servers := []GitServer{}
	if srv, err := http.FromLoader(l); err == nil {
		servers = append(servers, srv)
	}
	servers = append(servers, git.FromLoader(l))

	return servers
}
