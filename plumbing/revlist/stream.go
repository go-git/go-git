package revlist

import (
	"context"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Entry is one object discovered by Stream.
type Entry struct {
	Hash plumbing.Hash
	Type plumbing.ObjectType
}

// Stream walks the object graph the same way as Objects and yields each
// discovered object on out as it is found. It returns the final object
// count and closes out before returning. The walk runs in the caller's
// goroutine.
//
// out should be shallowly buffered (16-64 elements is typical). Consumers
// must drain promptly: a stalled consumer blocks enumeration.
//
// On error, the error is returned, out is closed, and the count value
// is undefined.
func Stream(ctx context.Context, s storer.EncodedObjectStorer, wants, haves []plumbing.Hash, out chan<- Entry) (int, error) {
	defer close(out)

	w, err := newObjectWalk(s)
	if err != nil {
		return 0, err
	}
	w.yield = func(h plumbing.Hash, t plumbing.ObjectType) error {
		select {
		case out <- Entry{Hash: h, Type: t}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := w.seedHaves(haves); err != nil {
		return 0, err
	}
	if err := w.seedWants(wants); err != nil {
		return 0, err
	}
	if err := w.walk(); err != nil {
		return 0, err
	}
	return w.count, nil
}
