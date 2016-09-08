package index

import (
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/idxfile"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
	"gopkg.in/src-d/go-git.v4/storage/memory"
)

// Index is a database of objects and their offset in a packfile.
// Objects are identified by their hash.
type Index map[core.Hash]int64

func New() Index {
	return make(Index)
}

// Decode decodes a idxfile into the Index
func (i *Index) Decode(r io.Reader) error {
	d := idxfile.NewDecoder(r)
	idx := &idxfile.Idxfile{}
	if err := d.Decode(idx); err != nil {
		return err
	}

	for _, e := range idx.Entries {
		fmt.Println(e.CRC32)
		(*i)[e.Hash] = int64(e.Offset)
	}

	return nil
}

// NewFrompackfile returns a new index from a packfile reader.
func NewFromPackfile(r io.Reader) (Index, core.Hash, error) {
	o := memory.NewStorage().ObjectStorage()
	s := packfile.NewScannerFromReader(r)
	d := packfile.NewDecoder(s, o)

	checksum, err := d.Decode()
	if err != nil {
		return nil, core.ZeroHash, err
	}

	index := Index(d.Offsets())
	return index, checksum, d.Close()
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
	/*if _, ok := i[h]; ok {
		return fmt.Errorf("index.Set failed: duplicated key: %s", h)
	}*/

	i[h] = o

	return nil
}
