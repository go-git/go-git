package main

import (
	"log"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"

	. "github.com/go-git/go-git/v5/_examples"
)

// Retrieve remote tags without cloning repository
func main() {
	CheckArgs("<url>")
	url := os.Args[1]

	Info("git ls-remote --tags %s", url)

	// Create the remote with repository URL
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})

	log.Print("Fetching tags...")

	// We can then use every Remote functions to retrieve wanted information
	refs, err := rem.List(&git.ListOptions{
		// Returns all references, including peeled references.
		PeelingOption: git.AppendPeeled,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Filters the references list and only keeps tags
	var tags []string
	for _, ref := range refs {
		if ref.Name().IsTag() {
			tags = append(tags, ref.Name().Short())
		}
	}

	if len(tags) == 0 {
		log.Println("No tags!")
		return
	}

	log.Printf("Tags found: %v", tags)
}
