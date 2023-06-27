package storer

import "github.com/sgnl-ai/go-git/plumbing/format/index"

// IndexStorer generic storage of index.Index
type IndexStorer interface {
	SetIndex(*index.Index) error
	Index() (*index.Index, error)
}
