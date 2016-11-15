package main

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/fatih/color"

	git "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/client"
	githttp "gopkg.in/src-d/go-git.v4/plumbing/client/http"
)

func main() {
	// Create a custom http(s) client
	customClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}, // accept any certificate (might be useful for testing)
		Timeout: 15 * time.Second, // 15 second timeout
		CheckRedirect: func(req *http.Request, via []*http.Request) error { // don't follow redirect
			return http.ErrUseLastResponse
		},
	}
	// Override http(s) default protocol to use our custom client
	clients.InstallProtocol(
		"https",
		githttp.NewGitUploadPackServiceFactory(customClient))

	// Create an in-memory repository
	r := git.NewMemoryRepository()

	const url = "https://github.com/git-fixtures/basic.git"

	// Clone repo
	if err := r.Clone(&git.CloneOptions{URL: url}); err != nil {
		panic(err)
	}

	// Retrieve the branch pointed by HEAD
	head, err := r.Head()
	if err != nil {
		panic(err)
	}

	// Print latest commit
	commit, err := r.Commit(head.Hash())
	if err != nil {
		panic(err)
	}
	color.Green(commit.String())
	// Output:
	// commit 6ecf0ef2c2dffb796033e5a02219af86ec6584e5
	// Author: Máximo Cuadros Ortiz <mcuadros@gmail.com>
	// Date:   Sun Apr 05 23:30:47 2015 +0200
	//
	//    vendor stuff
}
