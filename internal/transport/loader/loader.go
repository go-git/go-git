// Package loader holds the default transport.Loader that the file
// transport and the git server backend fall back to when no loader is
// configured.
//
// It lives apart from internal/transport because it imports
// plumbing/transport, which imports internal/transport.
package loader

import (
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

var defaultLoader transport.Loader = transport.NewFilesystemLoader(osfs.New(""), false)

// Default returns the shared filesystem loader that resolves URL paths
// against the root of the OS filesystem. It is not strict, so a path may
// name a work tree, a bare repository, or either with ".git" left off.
func Default() transport.Loader {
	return defaultLoader
}
