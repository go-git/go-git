package git

import (
	"errors"
	"fmt"

	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/formats/packfile"
)

var (
	ObjectNotFoundErr = errors.New("object not found")
)

const (
	DefaultRemoteName = "origin"
)

type Repository struct {
	Remotes map[string]*Remote
	Storage *core.RAWObjectStorage
}

// NewRepository creates a new repository setting remote as default remote
func NewRepository(url string, auth common.AuthMethod) (*Repository, error) {
	var remote *Remote
	var err error

	if auth == nil {
		remote, err = NewRemote(url)
	} else {
		remote, err = NewAuthenticatedRemote(url, auth)
	}

	if err != nil {
		return nil, err
	}

	r := NewPlainRepository()
	r.Remotes[DefaultRemoteName] = remote

	return r, nil
}

// NewPlainRepository creates a new repository without remotes
func NewPlainRepository() *Repository {
	return &Repository{
		Remotes: map[string]*Remote{},
		Storage: core.NewRAWObjectStorage(),
	}
}

func (r *Repository) Pull(remoteName, branch string) error {
	remote, ok := r.Remotes[remoteName]
	if !ok {
		return fmt.Errorf("unable to find remote %q", remoteName)
	}

	if err := remote.Connect(); err != nil {
		return err
	}

	ref, err := remote.Ref(branch)
	if err != nil {
		return err
	}

	reader, err := remote.Fetch(&common.GitUploadPackRequest{
		Want: []core.Hash{ref},
	})

	pr := packfile.NewReader(reader)
	_, err = pr.Read(r.Storage)

	if err != nil {
		return err
	}

	return nil
}

// Commit return the commit with the given hash
func (r *Repository) Commit(h core.Hash) (*Commit, error) {
	obj, ok := r.Storage.Get(h)
	if !ok {
		return nil, ObjectNotFoundErr
	}

	commit := &Commit{r: r}
	return commit, commit.Decode(obj)
}

// Commits decode the objects into commits
func (r *Repository) Commits() *CommitIter {
	i := NewCommitIter(r)
	go func() {
		defer i.Close()
		for _, obj := range r.Storage.Commits {
			i.Add(obj)
		}
	}()

	return i
}

// Tree return the tree with the given hash
func (r *Repository) Tree(h core.Hash) (*Tree, error) {
	obj, ok := r.Storage.Get(h)
	if !ok {
		return nil, ObjectNotFoundErr
	}

	tree := &Tree{r: r}
	return tree, tree.Decode(obj)
}
