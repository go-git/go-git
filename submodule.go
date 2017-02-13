package git

import (
	"srcd.works/go-git.v4/config"
	"srcd.works/go-git.v4/plumbing"
)

// Submodule a submodule allows you to keep another Git repository in a
// subdirectory of your repository.
type Submodule struct {
	m *config.Submodule
	w *Worktree
	// r is the submodule repository
	r *Repository
}

// Config returns the submodule config
func (s *Submodule) Config() *config.Submodule {
	return s.m
}

// Init initialize the submodule reading the recoreded Entry in the index for
// the given submodule
func (s *Submodule) Init() error {
	e, err := s.w.readIndexEntry(s.m.Path)
	if err != nil {
		return err
	}

	_, err = s.r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URL:  s.m.URL,
	})

	if err != nil {
		return err
	}

	return s.fetchAndCheckout(e.Hash)
}

// Update  the registered submodule to match what the superproject expects
func (s *Submodule) Update() error {
	e, err := s.w.readIndexEntry(s.m.Path)
	if err != nil {
		return err
	}

	return s.fetchAndCheckout(e.Hash)
}

func (s *Submodule) fetchAndCheckout(hash plumbing.Hash) error {
	if err := s.r.Fetch(&FetchOptions{}); err != nil && err != NoErrAlreadyUpToDate {
		return err
	}

	w, err := s.r.Worktree()
	if err != nil {
		return err
	}

	if err := w.Checkout(hash); err != nil {
		return err
	}

	head := plumbing.NewHashReference(plumbing.HEAD, hash)
	return s.r.Storer.SetReference(head)
}

// Submodules list of several submodules from the same repository
type Submodules []*Submodule

// Init initialize the submodule recorded in the index
func (s Submodules) Init() error {
	for _, sub := range s {
		if err := sub.Init(); err != nil {
			return err
		}
	}

	return nil
}
