package main

import (
	"os"

	"srcd.works/go-git.v4"
	. "srcd.works/go-git.v4/examples"
)

func main() {
	CheckArgs("<repository-path>")
	path := os.Args[1]

	r, err := git.PlainOpen(path)
	CheckIfError(err)

	Info("git push")
	err = r.Push(&git.PushOptions{})
	CheckIfError(err)
}
