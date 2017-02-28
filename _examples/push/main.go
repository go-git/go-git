package main

import (
	"os"

	"srcd.works/go-git.v4"
	. "srcd.works/go-git.v4/_examples"
)

// Example of how to open a repository in a specific path, and do a push to
// his default remote (origin).
func main() {
	CheckArgs("<repository-path>")
	path := os.Args[1]

	r, err := git.PlainOpen(path)
	CheckIfError(err)

	Info("git push")
	// push using default push options
	err = r.Push(&git.PushOptions{})
	CheckIfError(err)
}
