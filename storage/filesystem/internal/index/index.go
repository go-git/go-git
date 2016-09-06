package index

import (
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/idxfile"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
)

// Index is a database of objects and their offset in a packfile.
// Objects are identified by their hash.
type Index map[core.Hash]int64

// Decode decodes a idxfile into the Index
func (i *Index) Decode(r io.Reader) error {
	d := idxfile.NewDecoder(r)
	idx := &idxfile.Idxfile{}
	if err := d.Decode(idx); err != nil {
		return err
	}

	for _, e := range idx.Entries {
		(*i)[e.Hash] = int64(e.Offset)
	}

	return nil
}

// NewFrompackfile returns a new index from a packfile reader.
func NewFromPackfile(r io.Reader) (Index, core.Hash, error) {
	index := make(Index)

	p := packfile.NewParser(r)
	_, count, err := p.Header()
	if err != nil {
		return nil, core.ZeroHash, err
	}

	for i := 0; i < int(count); i++ {
		h, err := p.NextObjectHeader()
		if err = index.Set(core.ZeroHash, h.Offset); err != nil {
			return nil, core.ZeroHash, err
		}
	}

	hash, err := p.Checksum()
	return index, hash, err
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
