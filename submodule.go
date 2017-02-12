package git

import "srcd.works/go-git.v4/plumbing"

type Submodule struct {
	Name   string
	Branch string
	URL    string

	r *Repository
}

func (s *Submodule) Init() error {
	return s.r.clone(&CloneOptions{
		URL:           s.URL,
		ReferenceName: plumbing.ReferenceName(s.Branch),
	})
}

type Submodules []*Submodule

func (s Submodules) Init() error {
	for _, sub := range s {
		if err := sub.Init(); err != nil {
			return err
		}
	}

	return nil
}
