package storage

import (
	"errors"

	"github.com/go-git/go-git/v5/plumbing"
)

var ErrLimitExceeded = errors.New("limit exceeded")

// Limited wraps git.Storer to limit the number of bytes that can be stored.
type Limited struct {
	Storer
	N *int64
}

// Limit returns a git.Storer limited to the specified number of bytes.
func Limit(s Storer, n int64) *Limited {
	return &Limited{Storer: s, N: &n}
}

func (s *Limited) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	*s.N -= obj.Size()
	if *s.N < 0 {
		return plumbing.ZeroHash, ErrLimitExceeded
	}
	return s.Storer.SetEncodedObject(obj)
}

func (s *Limited) Module(name string) (Storer, error) {
	m, err := s.Storer.Module(name)
	if err != nil {
		return nil, err
	}
	return &Limited{Storer: m, N: s.N}, nil
}
