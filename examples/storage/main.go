package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aerospike/aerospike-client-go"

	"gopkg.in/src-d/go-git.v4"
)

func main() {
	url := os.Args[2]
	client, err := aerospike.NewClient("127.0.0.1", 3000)
	if err != nil {
		panic(err)
	}

	s, err := NewAerospikeStorage(client, "test", url)
	if err != nil {
		panic(err)
	}

	r, err := git.NewRepository(s)
	if err != nil {
		panic(err)
	}

	switch os.Args[1] {
	case "clone":
		clone(r, url)
	case "list":
		list(r)
	default:
		panic("unknown option")
	}
}

func clone(r *git.Repository, url string) {
	fmt.Printf("Cloning %q ...\n", os.Args[2])
	start := time.Now()

	if err := r.Clone(&git.CloneOptions{URL: url}); err != nil {
		panic(err)
	}

	fmt.Printf("Time elapsed %s\n", time.Since(start))
}

func list(r *git.Repository) {
	fmt.Printf("Listing commits from %q ...\n", os.Args[1])

	iter, err := r.Commits()
	if err != nil {
		panic(err)
	}
	defer iter.Close()

	var count int
	for {
		//the commits are not shorted in any special order
		commit, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			panic(err)
		}

		count++
		fmt.Println(commit)
	}

	fmt.Printf("Total number of commits %d\n", count)
}
