package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/examples"
	"gopkg.in/src-d/go-git.v4/examples/storage/aerospike"

	driver "github.com/aerospike/aerospike-client-go"
)

func main() {
	CheckArgs("<clone|log>", "<url>")
	action := os.Args[1]
	url := os.Args[2]

	// Aerospike client to be used by the custom storage
	client, err := driver.NewClient("127.0.0.1", 3000)
	CheckIfError(err)

	// New instance of the custom aerospike storage, all the objects,
	// references and configuration is saved to aerospike
	s, err := aerospike.NewStorage(client, "test", url)
	CheckIfError(err)

	switch action {
	case "clone":
		clone(s, url)
	case "log":
		log(s)
	default:
		panic("unknown option")
	}
}

func clone(s git.Storer, url string) {
	// Clone the given repository, all the objects, references and
	// configuration sush as remotes, are save into the Aerospike database
	// using the custom storer
	Info("git clone %s", url)

	_, err := git.Clone(s, nil, &git.CloneOptions{URL: url})
	CheckIfError(err)
}

func log(s git.Storer) {
	// We open the repository using as storer the custom implementation
	r, err := git.Open(s, nil)
	CheckIfError(err)

	// Prints the history of the repository starting in the current HEAD, the
	// objects are retrieved from Aerospike database.
	Info("git log --oneline")

	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.Commit(ref.Hash())
	CheckIfError(err)
	commits, err := commit.History()
	CheckIfError(err)

	for _, c := range commits {
		hash := c.Hash.String()
		line := strings.Split(c.Message, "\n")
		fmt.Println(hash[:7], line[0])
	}
}
