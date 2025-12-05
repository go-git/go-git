package server

import (
	"io"

	"github.com/go-git/go-git/v6/internal/server/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

type GitServer interface {
	Start() (string, error)
	io.Closer
}

func All(l transport.Loader) []GitServer {
	servers := []GitServer{}
	if srv, err := http.FromLoader(l); err == nil {
		servers = append(servers, srv)
	}

	return servers
}
