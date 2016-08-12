package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aerospike/aerospike-client-go"

	"gopkg.in/src-d/go-git.v3"
)

func main() {
	url := os.Args[2]
	r, err := git.NewRepository(url, nil)
	if err != nil {
		panic(err)
	}

	client, err := aerospike.NewClient("127.0.0.1", 3000)
	if err != nil {
		panic(err)
	}

	r.Storage = NewAerospikeObjectStorage(url, client)

	switch os.Args[1] {
	case "pull":
		pull(r)
	case "list":
		list(r)
	default:
		panic("unknown option")
	}
}

func pull(r *git.Repository) {
	fmt.Printf("Retrieving %q ...\n", os.Args[2])
	start := time.Now()

	if err := r.PullDefault(); err != nil {
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
