package index

import (
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/formats/idxfile"
	"gopkg.in/src-d/go-git.v3/formats/packfile"
)

// Index is a database of objects and their offset in a packfile.
// Objects are identified by their hash.
type Index map[core.Hash]int64

// NewFromIdx returns a new index from an idx file reader.
func NewFromIdx(r io.Reader) (Index, error) {
	d := idxfile.NewDecoder(r)
	idx := &idxfile.Idxfile{}
	err := d.Decode(idx)
	if err != nil {
		return nil, err
	}

	ind := make(Index)
	for _, e := range idx.Entries {
		if _, ok := ind[e.Hash]; ok {
			return nil, fmt.Errorf("duplicated hash: %s", e.Hash)
		}
		ind[e.Hash] = int64(e.Offset)
	}

	return ind, nil
}

// NewFrompackfile returns a new index from a packfile reader.
func NewFromPackfile(rs io.ReadSeeker) (Index, error) {
	index := make(Index)

	r := packfile.NewSeekable(rs)
	p := packfile.NewParser(r)

	count, err := p.ReadHeader()
	if err != nil {
		return nil, err
	}

	for i := 0; i < int(count); i++ {
		offset, err := r.Offset()
		if err != nil {
			return nil, err
		}

		obj, err := p.ReadObject()
		if err != nil {
			return nil, err
		}

		err = r.Remember(offset, obj)
		if err != nil {
			return nil, err
		}

		err = index.Set(obj.Hash(), offset)
		if err != nil {
			return nil, err
		}
	}

	return index, nil
}

// Get returns the offset that an object has the packfile.
func (i Index) Get(h core.Hash) (int64, error) {
	o, ok := i[h]
	if !ok {
		return 0, core.ErrObjectNotFound
	}

	return o, nil
}

// Set adds a new hash-offset pair to the index, or substitutes an existing one.
func (i Index) Set(h core.Hash, o int64) error {
	if _, ok := i[h]; ok {
		return fmt.Errorf("index.Set failed: duplicated key: %s", h)
	}

	i[h] = o

	return nil
}
