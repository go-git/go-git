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
func NewFromPackfile(rs io.ReadSeeker) (Index, core.Hash, error) {
	s := packfile.NewSeekable(rs)
	return newFromPackfile(rs, s)
}

func NewFromPackfileInMemory(rs io.Reader) (Index, core.Hash, error) {
	s := packfile.NewStream(rs)
	return newFromPackfile(rs, s)
}

func newFromPackfile(r io.Reader, s packfile.ReadRecaller) (Index, core.Hash, error) {
	index := make(Index)

	p := packfile.NewParser(s)
	count, err := p.ReadHeader()
	if err != nil {
		return nil, core.ZeroHash, err
	}

	for i := 0; i < int(count); i++ {
		offset, err := s.Offset()
		if err != nil {
			return nil, core.ZeroHash, err
		}

		obj := &core.MemoryObject{}
		if err := p.FillObject(obj); err != nil {
			return nil, core.ZeroHash, err
		}

		err = s.Remember(offset, obj)
		if err != nil {
			return nil, core.ZeroHash, err
		}

		if err = index.Set(obj.Hash(), offset); err != nil {
			return nil, core.ZeroHash, err
		}
	}

	//The trailer records 20-byte SHA-1 checksum of all of the above.
	hash, err := p.ReadHash()
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
