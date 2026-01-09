package storer

import "github.com/go-git/go-billy/v6"

type WorktreeStorer interface {
	Filesystem() billy.Filesystem
}
