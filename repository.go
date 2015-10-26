package git

import (
	"fmt"

	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/formats/packfile"
	"gopkg.in/src-d/go-git.v2/internal"
)

const (
	DefaultRemoteName = "origin"
)

type Repository struct {
	Remotes map[string]*Remote
	Storage *internal.RAWObjectStorage
}

// NewRepository creates a new repository setting remote as default remote
func NewRepository(url string) (*Repository, error) {
	remote, err := NewRemote(url)
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
		Storage: internal.NewRAWObjectStorage(),
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
		Want: []internal.Hash{ref},
	})

	pr := packfile.NewReader(reader)
	_, err = pr.Read(r.Storage)

	if err != nil {
		return err
	}

	return nil
}
