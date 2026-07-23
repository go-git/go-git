package git_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v6/memfs"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/storage/memory"
)

func ExampleClone() {
	// Filesystem abstraction based on memory
	fs := memfs.New()
	// Git objects storer based on memory
	storer := memory.NewStorage()

	// Clones the repository into the worktree (fs) and stores all the .git
	// content into the storer
	r, err := git.Clone(context.Background(), storer, fs, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	// Prints the content of the CHANGELOG file from the cloned repository
	changelog, err := fs.Open("CHANGELOG")
	if err != nil {
		log.Print(err)
		return
	}

	io.Copy(os.Stdout, changelog)
	// Output: Initial changelog
}

func ExamplePlainClone() {
	// Tempdir to clone the repository
	dir, err := os.MkdirTemp("", "clone-example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir) // clean up

	// Clones the repository into the given dir, just as a normal git clone does
	r, err := git.PlainClone(context.Background(), dir, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})
	if err != nil {
		log.Print(err)
		return
	}
	defer func() { _ = r.Close() }()

	// Prints the content of the CHANGELOG file from the cloned repository
	changelog, err := os.Open(filepath.Join(dir, "CHANGELOG"))
	if err != nil {
		log.Print(err)
		return
	}

	io.Copy(os.Stdout, changelog)
	// Output: Initial changelog
}

func ExamplePlainClone_usernamePassword() {
	// Tempdir to clone the repository
	dir, err := os.MkdirTemp("", "clone-example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir) // clean up

	// Clones the repository into the given dir, just as a normal git clone does
	r, err := git.PlainClone(context.Background(), dir, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
		ClientOptions: []client.Option{
			client.WithHTTPAuth(&http.BasicAuth{
				Username: "username",
				Password: "password",
			}),
		},
	})
	if err != nil {
		log.Print(err)
		return
	}
	defer func() { _ = r.Close() }()
}

func ExamplePlainClone_accessToken() {
	// Tempdir to clone the repository
	dir, err := os.MkdirTemp("", "clone-example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir) // clean up

	// Clones the repository into the given dir, just as a normal git clone does
	r, err := git.PlainClone(context.Background(), dir, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
		ClientOptions: []client.Option{
			client.WithHTTPAuth(&http.BasicAuth{
				Username: "abc123", // anything except an empty string
				Password: "github_access_token",
			}),
		},
	})
	if err != nil {
		log.Print(err)
		return
	}
	defer func() { _ = r.Close() }()
}

func ExampleRepository_References() {
	ctx := context.Background()
	r, _ := git.Clone(ctx, memory.NewStorage(), nil, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})
	defer func() { _ = r.Close() }()

	// simulating a git show-ref
	refs, _ := r.References(ctx)
	refs.ForEach(ctx, func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.HashReference {
			fmt.Println(ref)
		}

		return nil
	})

	// Example Output:
	// 6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/master
	// e8d3ffab552895c19b9fcf7aa264d277cde33881 refs/remotes/origin/branch
	// 6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master
}

func ExampleRepository_Branches() {
	ctx := context.Background()
	r, _ := git.Clone(ctx, memory.NewStorage(), nil, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})
	defer func() { _ = r.Close() }()

	branches, _ := r.Branches(ctx)
	branches.ForEach(ctx, func(branch *plumbing.Reference) error {
		fmt.Println(branch.Hash().String(), branch.Name())
		return nil
	})

	// Example Output:
	// 6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master
}

func ExampleRepository_CreateRemote() {
	ctx := context.Background()
	r, _ := git.Init(ctx, memory.NewStorage())
	defer func() { _ = r.Close() }()

	// Add a new remote, with the default fetch refspec
	_, err := r.CreateRemote(ctx, &config.RemoteConfig{
		Name: "example",
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})
	if err != nil {
		log.Print(err)
		return
	}

	list, err := r.Remotes(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	for _, r := range list {
		fmt.Println(r)
	}

	// Example Output:
	// example https://github.com/git-fixtures/basic.git (fetch)
	// example https://github.com/git-fixtures/basic.git (push)
}
