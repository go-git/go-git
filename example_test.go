package git_test

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/go-git/go-billy/v5/memfs"
)

func ExampleClone() {
	// Filesystem abstraction based on memory
	fs := memfs.New()
	// Git objects storer based on memory
	storer := memory.NewStorage()

	// Clones the repository into the worktree (fs) and stores all the .git
	// content into the storer
	_, err := git.Clone(storer, fs, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Prints the content of the CHANGELOG file from the cloned repository
	changelog, err := fs.Open("CHANGELOG")
	if err != nil {
		log.Fatal(err)
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
	_, err = git.PlainClone(dir, false, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})

	if err != nil {
		log.Fatal(err)
	}

	// Prints the content of the CHANGELOG file from the cloned repository
	changelog, err := os.Open(filepath.Join(dir, "CHANGELOG"))
	if err != nil {
		log.Fatal(err)
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
	_, err = git.PlainClone(dir, false, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
		Auth: &http.BasicAuth{
			Username: "username",
			Password: "password",
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}

func ExamplePlainClone_accessToken() {
	// Tempdir to clone the repository
	dir, err := os.MkdirTemp("", "clone-example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir) // clean up

	// Clones the repository into the given dir, just as a normal git clone does
	_, err = git.PlainClone(dir, false, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
		Auth: &http.BasicAuth{
			Username: "abc123", // anything except an empty string
			Password: "github_access_token",
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}

func ExampleRepository_References() {
	r, _ := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: "https://github.com/git-fixtures/basic.git",
	})

	// simulating a git show-ref
	refs, _ := r.References()
	refs.ForEach(func(ref *plumbing.Reference) error {
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

func ExampleRepository_CreateRemote() {
	r, _ := git.Init(memory.NewStorage(), nil)

	// Add a new remote, with the default fetch refspec
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "example",
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})

	if err != nil {
		log.Fatal(err)
	}

	list, err := r.Remotes()
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range list {
		fmt.Println(r)
	}

	// Example Output:
	// example https://github.com/git-fixtures/basic.git (fetch)
	// example https://github.com/git-fixtures/basic.git (push)
}
