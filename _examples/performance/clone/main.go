package main

import (
	"crypto"
	"crypto/sha1"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing/hash"
	"github.com/go-git/go-git/v5/utils/trace"
)

// Expands the Basic example focusing in performance.
func main() {
	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]

	// Replace sha1cd with Golang's sha1 implementation, which is faster.
	// SHA1 as a hash algorithm is broken, so Git implementations tend to use
	// an alternative implementation that includes collision detection - which
	// is the default on go-git and in the git cli.
	//
	// This operation is only safe when interacting with trustworthy Git servers,
	// such as GitHub and GitLab. If your application needs to interact with
	// custom servers or does not impose any sort of constraints on the target
	// server, this is not recommended.
	hash.RegisterHash(crypto.SHA1, sha1.New)

	// Clone the given repository to the given directory
	Info("GIT_TRACE_PERFORMANCE=true git clone --no-tags --depth 1 --single-branch %s %s", url, directory)

	// Enable performance metrics. This is only to show the break down per
	// operation, and can be removed. Like in the git CLI, this can be enabled
	// at runtime by environment variable:
	//   GIT_TRACE_PERFORMANCE=true
	trace.SetTarget(trace.Performance)

	r, err := git.PlainClone(directory, false, &git.CloneOptions{
		URL: url,
		// Differently than the git CLI, by default go-git downloads
		// all tags and its related objects. To avoid unnecessary
		// data transmission and processing, opt-out tags.
		Tags: git.NoTags,
		// Shallow clones the repository, returning a single commit.
		Depth: 1,
		// Depth 1 implies single branch, so this is largely redundant.
		SingleBranch: true,
		// Not a net positive change for performance, this was added
		// to better align the output when compared with the git CLI.
		Progress: os.Stdout,
	})

	CheckIfError(err)

	ref, err := r.Head()
	CheckIfError(err)
	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
