package git

import (
	"fmt"

	"srcd.works/go-git.v4/plumbing"
)

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
		fmt.Println("clone", sub.URL)
		if err := sub.Init(); err != nil {
			fmt.Println(err)
			return err
		}
	}

	return nil
}
