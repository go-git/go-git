package main

import (
	"crypto"
	"crypto/sha1"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/trace"
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

	fs := osfs.New(directory)
	dotgit, err := fs.Chroot(".git")
	CheckIfError(err)

	storer := filesystem.NewStorageWithOptions(dotgit, cache.NewObjectLRUDefault(), filesystem.Options{
		// HighMemoryMode caches delta objects in memory, so that they don't
		// need to be inflated on demand. This decreases execution time but
		// could increase memory usage quite considerably.
		// This was the default before v6.
		HighMemoryMode: true,
	})

	r, err := git.Clone(storer, fs, &git.CloneOptions{
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
