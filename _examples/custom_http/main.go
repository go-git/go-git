package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/storage/memory"
)

func main() {
	CheckArgs("<url>")
	url := os.Args[1]

	customClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	Info("git clone %s", url)

	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: url,
		ClientOptions: []client.Option{
			client.WithHTTPClient(customClient),
		},
	})
	CheckIfError(err)

	Info("git rev-parse HEAD")

	head, err := r.Head()
	CheckIfError(err)
	fmt.Println(head.Hash())
}
