package git

import (
	"errors"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

var (
	ErrSubmoduleAlreadyInitialized = errors.New("submodule already initialized")
	ErrSubmoduleNotInitialized     = errors.New("submodule not initialized")
)

// Submodule a submodule allows you to keep another Git repository in a
// subdirectory of your repository.
type Submodule struct {
	initialized bool

	c *config.Submodule
	w *Worktree
}

// Config returns the submodule config
func (s *Submodule) Config() *config.Submodule {
	return s.c
}

// Init initialize the submodule reading the recoreded Entry in the index for
// the given submodule
func (s *Submodule) Init() error {
	cfg, err := s.w.r.Storer.Config()
	if err != nil {
		return err
	}

	_, ok := cfg.Submodules[s.c.Name]
	if ok {
		return ErrSubmoduleAlreadyInitialized
	}

	s.initialized = true

	cfg.Submodules[s.c.Name] = s.c
	return s.w.r.Storer.SetConfig(cfg)
}

// Repository returns the Repository represented by this submodule
func (s *Submodule) Repository() (*Repository, error) {
	storer, err := s.w.r.Storer.Module(s.c.Name)
	if err != nil {
		return nil, err
	}

	_, err = storer.Reference(plumbing.HEAD)
	if err != nil && err != plumbing.ErrReferenceNotFound {
		return nil, err
	}

	worktree := s.w.fs.Dir(s.c.Path)
	if err == nil {
		return Open(storer, worktree)
	}

	r, err := Init(storer, worktree)
	if err != nil {
		return nil, err
	}

	_, err = r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URL:  s.c.URL,
	})

	return r, err
}

// Update the registered submodule to match what the superproject expects, the
// submodule should be initilized first calling the Init method or setting in
// the options SubmoduleUpdateOptions.Init equals true
func (s *Submodule) Update(o *SubmoduleUpdateOptions) error {
	if !s.initialized && !o.Init {
		return ErrSubmoduleNotInitialized
	}

	if !s.initialized && o.Init {
		if err := s.Init(); err != nil {
			return err
		}
	}

	e, err := s.w.readIndexEntry(s.c.Path)
	if err != nil {
		return err
	}

	r, err := s.Repository()
	if err != nil {
		return err
	}

	if err := s.fetchAndCheckout(r, o, e.Hash); err != nil {
		return err
	}

	return s.doRecrusiveUpdate(r, o)
}

func (s *Submodule) doRecrusiveUpdate(r *Repository, o *SubmoduleUpdateOptions) error {
	if o.RecurseSubmodules == NoRecurseSubmodules {
		return nil
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	l, err := w.Submodules()
	if err != nil {
		return err
	}

	new := &SubmoduleUpdateOptions{}
	*new = *o
	new.RecurseSubmodules--
	return l.Update(new)
}

func (s *Submodule) fetchAndCheckout(r *Repository, o *SubmoduleUpdateOptions, hash plumbing.Hash) error {
	if !o.NoFetch {
		err := r.Fetch(&FetchOptions{})
		if err != nil && err != NoErrAlreadyUpToDate {
			return err
		}
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	if err := w.Checkout(hash); err != nil {
		return err
	}

	head := plumbing.NewHashReference(plumbing.HEAD, hash)
	return r.Storer.SetReference(head)
}

// Submodules list of several submodules from the same repository
type Submodules []*Submodule

// Init initializes the submodules in this list
func (s Submodules) Init() error {
	for _, sub := range s {
		if err := sub.Init(); err != nil {
			return err
		}
	}

	return nil
}

// Update updates all the submodules in this list
func (s Submodules) Update(o *SubmoduleUpdateOptions) error {
	for _, sub := range s {
		if err := sub.Update(o); err != nil {
			return err
		}
	}

	return nil
}
