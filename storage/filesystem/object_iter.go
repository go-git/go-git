package filesystem

import (
	"bytes"
	"crypto"
	"fmt"
	"io"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type lazyPackfilesIter struct {
	hashes []plumbing.Hash
	open   func(h plumbing.Hash) (storer.EncodedObjectIter, error)
	cur    storer.EncodedObjectIter
}

func (it *lazyPackfilesIter) Next() (plumbing.EncodedObject, error) {
	for {
		if it.cur == nil {
			if len(it.hashes) == 0 {
				return nil, io.EOF
			}
			h := it.hashes[0]
			it.hashes = it.hashes[1:]

			sub, err := it.open(h)
			if err == io.EOF {
				continue
			} else if err != nil {
				return nil, err
			}
			it.cur = sub
		}
		ob, err := it.cur.Next()
		if err == io.EOF {
			it.cur.Close()
			it.cur = nil
			continue
		} else if err != nil {
			return nil, err
		}
		return ob, nil
	}
}

func (it *lazyPackfilesIter) ForEach(cb func(plumbing.EncodedObject) error) error {
	return storer.ForEachIterator(it, cb)
}

func (it *lazyPackfilesIter) Close() {
	if it.cur != nil {
		it.cur.Close()
		it.cur = nil
	}
	it.hashes = nil
}

type packfileIter struct {
	pack billy.File
	iter storer.EncodedObjectIter
	seen map[plumbing.Hash]struct{}

	// tells whether the pack file should be left open after iteration or not
	keepPack bool
}

// NewPackfileIter returns a new EncodedObjectIter for the provided packfile
// and object type. Packfile and index file will be closed after they're
// used. If keepPack is true the packfile won't be closed after the iteration
// finished.
func NewPackfileIter(
	fs billy.Filesystem,
	f billy.File,
	idxFile billy.File,
	t plumbing.ObjectType,
	keepPack bool,
	_ int64, // largeObjectThreshold - currently unused
	objectIDSize int,
) (storer.EncodedObjectIter, error) {
	// Read all idx bytes upfront so we can build multiple readers from them.
	defer idxFile.Close()
	idxBytes, err := io.ReadAll(io.LimitReader(idxFile, idxfile.MaxIdxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(idxBytes) > idxfile.MaxIdxFileSize {
		return nil, fmt.Errorf("index file too large (>%d bytes)", idxfile.MaxIdxFileSize)
	}

	// newHasher returns a fresh hash.Hash appropriate for objectIDSize.
	newHasher := func() hash.Hash {
		if objectIDSize == crypto.SHA256.Size() {
			return hash.New(crypto.SHA256)
		}
		return hash.New(crypto.SHA1)
	}

	// Triple-decode trade-off: we decode a temporary MemoryIndex here just
	// to extract the PackfileChecksum, which NewLazyIndex needs for .rev
	// header validation. DecodeLazy then re-reads the same bytes to build
	// its own lazy structures. A dedicated ReadPackChecksum helper would
	// avoid this extra decode but the cost is negligible. Note that
	// NewPackfileIter is exported and used by external consumers.
	tmpIdx := idxfile.NewMemoryIndex(objectIDSize)
	if err := idxfile.NewDecoder(bytes.NewReader(idxBytes), newHasher()).Decode(tmpIdx); err != nil {
		return nil, err
	}
	packHash := tmpIdx.PackfileChecksum

	// Pre-compute the rev bytes once; the closure just wraps
	// the pre-computed slice on each call.
	var revBuf bytes.Buffer
	if err := revfile.Encode(&revBuf, newHasher(), tmpIdx, tmpIdx.PackfileChecksum); err != nil {
		return nil, err
	}
	revBytes := revBuf.Bytes()

	revOpener := func() (idxfile.ReadAtCloser, error) {
		return idxfile.NewBytesReadAtCloser(revBytes), nil
	}

	idx, err := idxfile.DecodeLazy(bytes.NewReader(idxBytes), newHasher(), revOpener, packHash)
	if err != nil {
		return nil, err
	}

	seen := make(map[plumbing.Hash]struct{})
	return newPackfileIter(fs, f, t, seen, idx, nil, keepPack, objectIDSize)
}

func newPackfileIter(
	fs billy.Filesystem,
	f billy.File,
	t plumbing.ObjectType,
	seen map[plumbing.Hash]struct{},
	index idxfile.Index,
	cache cache.Object,
	keepPack bool,
	objectIDSize int,
) (storer.EncodedObjectIter, error) {
	p := packfile.NewPackfile(f,
		packfile.WithFs(fs),
		packfile.WithCache(cache),
		packfile.WithIdx(index),
		packfile.WithObjectIDSize(objectIDSize),
	)

	iter, err := p.GetByType(t)
	if err != nil {
		return nil, err
	}

	return &packfileIter{
		pack:     f,
		iter:     iter,
		seen:     seen,
		keepPack: keepPack,
	}, nil
}

func (iter *packfileIter) Next() (plumbing.EncodedObject, error) {
	for {
		obj, err := iter.iter.Next()
		if err != nil {
			return nil, err
		}

		if _, ok := iter.seen[obj.Hash()]; ok {
			continue
		}

		return obj, nil
	}
}

func (iter *packfileIter) ForEach(cb func(plumbing.EncodedObject) error) error {
	for {
		o, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				iter.Close()
				return nil
			}
			return err
		}

		if err := cb(o); err != nil {
			return err
		}
	}
}

func (iter *packfileIter) Close() {
	iter.iter.Close()
	if !iter.keepPack {
		_ = iter.pack.Close()
	}
}

type objectsIter struct {
	s *ObjectStorage
	t plumbing.ObjectType
	h []plumbing.Hash
}

func (iter *objectsIter) Next() (plumbing.EncodedObject, error) {
	if len(iter.h) == 0 {
		return nil, io.EOF
	}

	obj, err := iter.s.getFromUnpacked(iter.h[0])
	iter.h = iter.h[1:]

	if err != nil {
		return nil, err
	}

	if iter.t != plumbing.AnyObject && iter.t != obj.Type() {
		return iter.Next()
	}

	return obj, err
}

func (iter *objectsIter) ForEach(cb func(plumbing.EncodedObject) error) error {
	for {
		o, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if err := cb(o); err != nil {
			return err
		}
	}
}

func (iter *objectsIter) Close() {
	iter.h = []plumbing.Hash{}
}
