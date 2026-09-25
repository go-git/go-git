package reference

import (
	"errors"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Source yields references in ascending byte-wise name order, returning
// io.EOF when done.
type Source interface {
	Next() (*plumbing.Reference, error)
	Close()
}

// NewOverlayIter merges two sorted sources into one sorted iterator. On equal
// names it yields front's reference and skips back's, like Git's overlay
// iterator:
// https://github.com/git/git/blob/0f8e75abebff0877cae681a3d5ff31ac47f54220/refs/iterator.c#L273-L292
// A name repeated within a source is yielded once, at its first occurrence.
func NewOverlayIter(front, back Source) storer.ReferenceIter {
	return &overlayIter{front: overlaySide{src: front}, back: overlaySide{src: back}}
}

type overlaySide struct {
	src  Source
	next *plumbing.Reference
	done bool
}

// peek fills next unless the source is exhausted, closing it once it is.
func (s *overlaySide) peek() error {
	if s.next != nil || s.done {
		return nil
	}

	ref, err := s.src.Next()
	if err == io.EOF {
		s.src.Close()
		s.done = true
		return nil
	}
	s.next = ref
	return err
}

type overlayIter struct {
	front, back overlaySide
	last        plumbing.ReferenceName
	started     bool
}

// Next returns the next reference, or io.EOF once all have been returned.
func (iter *overlayIter) Next() (*plumbing.Reference, error) {
	for {
		if err := iter.front.peek(); err != nil {
			return nil, err
		}
		if err := iter.back.peek(); err != nil {
			return nil, err
		}

		f, b := iter.front.next, iter.back.next
		var ref *plumbing.Reference
		switch {
		case f == nil && b == nil:
			iter.Close()
			return nil, io.EOF
		case b == nil || (f != nil && f.Name() <= b.Name()):
			ref = f
			iter.front.next = nil
			if b != nil && b.Name() == f.Name() {
				iter.back.next = nil
			}
		default:
			ref = b
			iter.back.next = nil
		}

		if iter.started && ref.Name() == iter.last {
			continue
		}
		iter.started = true
		iter.last = ref.Name()
		return ref, nil
	}
}

// ForEach calls cb for each reference and closes the iterator.
// Returning storer.ErrStop stops iteration without an error.
func (iter *overlayIter) ForEach(cb func(*plumbing.Reference) error) error {
	defer iter.Close()
	for {
		ref, err := iter.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := cb(ref); err != nil {
			if errors.Is(err, storer.ErrStop) {
				return nil
			}

			return err
		}
	}
}

// Close closes both sources. Later calls to Next return io.EOF.
func (iter *overlayIter) Close() {
	for _, s := range []*overlaySide{&iter.front, &iter.back} {
		if !s.done {
			s.src.Close()
		}
		s.done = true
		s.next = nil
	}
}
