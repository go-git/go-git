package storer

import "github.com/grahambrooks/go-git/v5/plumbing/format/index"

// IndexStorer generic storage of index.Index
type IndexStorer interface {
	SetIndex(*index.Index) error
	Index() (*index.Index, error)
}
