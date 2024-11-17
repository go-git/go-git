package packfile

import (
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
)

type objectIter struct {
	p    *Packfile
	typ  plumbing.ObjectType
	iter idxfile.EntryIter
}

func (i *objectIter) Next() (plumbing.EncodedObject, error) {
	if err := i.p.init(); err != nil {
		return nil, err
	}

	i.p.m.Lock()
	defer i.p.m.Unlock()

	return i.next()
}

func (i *objectIter) next() (plumbing.EncodedObject, error) {
	for {
		e, err := i.iter.Next()
		if err != nil {
			return nil, err
		}

		oh, err := i.p.headerFromOffset(int64(e.Offset))
		if err != nil {
			return nil, err
		}

		if i.typ == plumbing.AnyObject {
			return i.p.objectFromHeader(oh)
		}

		// Current object header type is a delta, get the actual object to
		// assess the actual type.
		if oh.Type.IsDelta() {
			o, err := i.p.objectFromHeader(oh)
			if o.Type() == i.typ {
				return o, err
			}

			continue
		}

		if oh.Type == i.typ {
			return i.p.objectFromHeader(oh)
		}

		continue
	}
}

func (i *objectIter) ForEach(f func(plumbing.EncodedObject) error) error {
	if err := i.p.init(); err != nil {
		return err
	}

	i.p.m.Lock()
	defer i.p.m.Unlock()

	for {
		o, err := i.next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if err := f(o); err != nil {
			return err
		}
	}
}

func (i *objectIter) Close() {
	i.p.m.Lock()
	defer i.p.m.Unlock()

	i.iter.Close()
}
