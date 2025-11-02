package main

import (
	"os"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	. "github.com/go-git/go-git/v6/_examples"
)

// Updates server info (info/refs & objects/info/packs)
// files in a repository. Git http transport (dumb) uses them
// to generate a list of available refs for the repository.
// https://git-scm.com/docs/git-update-server-info
// https://git-scm.com/book/id/v2/Git-Internals-Transfer-Protocols#_the_dumb_protocol
func main() {
	CheckArgs("<path>")
	path := os.Args[1]

	// We instantiate a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	CheckIfError(err)

	// Update the server info files & save them to the file-system.
	fs := r.Storer.(*filesystem.Storage).Filesystem()
	err = transport.UpdateServerInfo(r.Storer, fs)
	CheckIfError(err)
}
