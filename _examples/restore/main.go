package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func prepareRepo(w *git.Worktree, directory string) {
	// We need a known state of files inside the worktree for testing revert a modify and delete
	Info("echo \"hello world! Modify\" > for-modify")
	err := ioutil.WriteFile(filepath.Join(directory, "for-modify"), []byte("hello world! Modify"), 0644)
	CheckIfError(err)
	Info("git add for-modify")
	_, err = w.Add("for-modify")
	CheckIfError(err)

	Info("echo \"hello world! Delete\" > for-delete")
	err = ioutil.WriteFile(filepath.Join(directory, "for-delete"), []byte("hello world! Delete"), 0644)
	CheckIfError(err)
	Info("git add for-delete")
	_, err = w.Add("for-delete")
	CheckIfError(err)

	Info("git commit -m \"example go-git commit\"")
	_, err = w.Commit("example go-git commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@doe.org",
			When:  time.Now(),
		},
	})
	CheckIfError(err)
}

// An example of how to restore AKA unstage files
func main() {
	CheckArgs("<directory>")
	directory := os.Args[1]

	// Opens an already existing repository.
	r, err := git.PlainOpen(directory)
	CheckIfError(err)

	w, err := r.Worktree()
	CheckIfError(err)

	prepareRepo(w, directory)

	// Perform the operation and stage them
	Info("echo \"hello world! Modify 2\" > for-modify")
	err = ioutil.WriteFile(filepath.Join(directory, "for-modify"), []byte("hello world! Modify 2"), 0644)
	CheckIfError(err)
	Info("git add for-modify")
	_, err = w.Add("for-modify")
	CheckIfError(err)

	Info("echo \"hello world! Add\" > for-add")
	err = ioutil.WriteFile(filepath.Join(directory, "for-add"), []byte("hello world! Add"), 0644)
	CheckIfError(err)
	Info("git add for-add")
	_, err = w.Add("for-add")
	CheckIfError(err)

	Info("rm for-delete")
	err = os.Remove(filepath.Join(directory, "for-delete"))
	CheckIfError(err)
	Info("git add for-delete")
	_, err = w.Add("for-delete")
	CheckIfError(err)

	// We can verify the current status of the worktree using the method Status.
	Info("git status --porcelain")
	status, err := w.Status()
	CheckIfError(err)
	fmt.Println(status)

	// Unstage a single file and see the status
	Info("git restore --staged for-modify")
	err = w.Restore(&git.RestoreOptions{Staged: true, Files: []string{"for-modify"}})
	CheckIfError(err)

	Info("git status --porcelain")
	status, err = w.Status()
	CheckIfError(err)
	fmt.Println(status)

	// Unstage the other 2 files and see the status
	Info("git restore --staged for-add for-delete")
	err = w.Restore(&git.RestoreOptions{Staged: true, Files: []string{"for-add", "for-delete"}})
	CheckIfError(err)

	Info("git status --porcelain")
	status, err = w.Status()
	CheckIfError(err)
	fmt.Println(status)
}
