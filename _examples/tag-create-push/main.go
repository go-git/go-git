package main

import (
	"fmt"
	"log"
	"os"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Example of how create a tag and push it to a remote.
func main() {
	CheckArgs("<ssh-url>", "<directory>", "<tag>", "<name>", "<email>", "<public-key>")
	url := os.Args[1]
	directory := os.Args[2]
	tag := os.Args[3]
	key := os.Args[6]

	r, err := cloneRepo(url, directory, key)

	if err != nil {
		log.Printf("clone repo error: %s", err)
		return
	}

	created, err := setTag(r, tag)
	if err != nil {
		log.Printf("create tag error: %s", err)
		return
	}

	if created {
		err = pushTags(r, key)
		if err != nil {
			log.Printf("push tag error: %s", err)
			return
		}
	}
}

func cloneRepo(url, dir, publicKeyPath string) (*git.Repository, error) {
	log.Printf("cloning %s into %s", url, dir)
	auth, keyErr := publicKey(publicKeyPath)
	if keyErr != nil {
		return nil, keyErr
	}

	Info("git clone %s", url)
	r, err := git.PlainClone(dir, false, &git.CloneOptions{
		Progress: os.Stdout,
		URL:      url,
		Auth:     auth,
	})

	if err != nil {
		log.Printf("clone git repo error: %s", err)
		return nil, err
	}

	return r, nil
}

func publicKey(filePath string) (*ssh.PublicKeys, error) {
	var publicKey *ssh.PublicKeys
	sshKey, _ := os.ReadFile(filePath)
	publicKey, err := ssh.NewPublicKeys("git", []byte(sshKey), "")
	if err != nil {
		return nil, err
	}
	return publicKey, err
}

func tagExists(tag string, r *git.Repository) bool {
	tagFoundErr := "tag was found"
	Info("git show-ref --tag")
	tags, err := r.TagObjects()
	if err != nil {
		log.Printf("get tags error: %s", err)
		return false
	}
	res := false
	err = tags.ForEach(func(t *object.Tag) error {
		if t.Name == tag {
			res = true
			return fmt.Errorf(tagFoundErr)
		}
		return nil
	})
	if err != nil && err.Error() != tagFoundErr {
		log.Printf("iterate tags error: %s", err)
		return false
	}
	return res
}

func setTag(r *git.Repository, tag string) (bool, error) {
	if tagExists(tag, r) {
		log.Printf("tag %s already exists", tag)
		return false, nil
	}
	log.Printf("Set tag %s", tag)
	h, err := r.Head()
	if err != nil {
		log.Printf("get HEAD error: %s", err)
		return false, err
	}
	Info("git tag -a %s %s -m \"%s\"", tag, h.Hash(), tag)
	_, err = r.CreateTag(tag, h.Hash(), &git.CreateTagOptions{
		Message: tag,
	})

	if err != nil {
		log.Printf("create tag error: %s", err)
		return false, err
	}

	return true, nil
}

func pushTags(r *git.Repository, publicKeyPath string) error {

	auth, _ := publicKey(publicKeyPath)

	po := &git.PushOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/tags/*:refs/tags/*")},
		Auth:       auth,
	}
	Info("git push --tags")
	err := r.Push(po)

	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			log.Print("origin remote was up to date, no push done")
			return nil
		}
		log.Printf("push to remote origin error: %s", err)
		return err
	}

	return nil
}
