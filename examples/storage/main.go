package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/examples/storage/aerospike"

	driver "github.com/aerospike/aerospike-client-go"
	"github.com/fatih/color"
)

func main() {
	checkArgs()
	action := os.Args[1]
	url := os.Args[2]

	// Aerospike client to be used by the custom storage
	client, err := driver.NewClient("127.0.0.1", 3000)
	checkIfError(err)

	// New instance of the custom aerospike storage, all the objects,
	// references and configuration is saved to aerospike
	s, err := aerospike.NewStorage(client, "test", url)
	checkIfError(err)

	// A new repository instance using as storage the custom implementation
	r, err := git.NewRepository(s)
	checkIfError(err)

	switch action {
	case "clone":
		clone(r, url)
	case "log":
		log(r)
	default:
		panic("unknown option")
	}
}

func clone(r *git.Repository, url string) {
	// Clone the given repository, all the objects, references and
	// configuration sush as remotes, are save into the Aerospike database.
	// > git clone <url>
	color.Blue("git clone %s", url)
	err := r.Clone(&git.CloneOptions{URL: url})
	checkIfError(err)
}

func log(r *git.Repository) {
	// Prints the history of the repository starting in the current HEAD, the
	// objects are retrieved from Aerospike database.
	// > git log --oneline
	color.Blue("git log --oneline")

	ref, err := r.Head()
	checkIfError(err)
	commit, err := r.Commit(ref.Hash())
	checkIfError(err)
	commits, err := commit.History()
	checkIfError(err)

	for _, c := range commits {
		hash := c.Hash.String()
		line := strings.Split(c.Message, "\n")
		fmt.Println(hash[:7], line[0])
	}
}

func checkIfError(err error) {
	if err == nil {
		return
	}

	color.Red("error: %s", err)
	os.Exit(1)
}

func checkArgs() {
	if len(os.Args) < 3 {
		color.Cyan("Usage: %s <clone|log> <url>", os.Args[0])
		os.Exit(1)
	}
}
