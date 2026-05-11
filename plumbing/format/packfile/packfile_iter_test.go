package packfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

func TestObjectIterReturnsMalformedDeltaError(t *testing.T) {
	t.Parallel()

	delta := buildDelta(1, 1, insertOp([]byte("b")))
	missingReference := plumbing.NewHash("1111111111111111111111111111111111111111")

	for _, test := range []struct {
		name string
		obj  testPackObject
	}{
		{
			name: "ref delta",
			obj: testPackObject{
				typ:       plumbing.REFDeltaObject,
				content:   delta,
				reference: missingReference,
			},
		},
		{
			name: "ofs delta",
			obj: testPackObject{
				typ:                 plumbing.OFSDeltaObject,
				content:             delta,
				offsetDeltaDistance: 1,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pack, offsets := buildTestPack(t, test.obj)
			file := writeTestPackFile(t, pack)
			entry := &idxfile.Entry{
				Hash:   testObjectHash(plumbing.BlobObject, []byte("unused")),
				Offset: uint64(offsets[0]),
			}
			p := NewPackfile(file, WithIdx(&singleEntryIndex{entry: entry}))
			defer p.Close()

			iter, err := p.GetByType(plumbing.BlobObject)
			require.NoError(t, err)
			defer iter.Close()

			obj, err := iter.Next()
			require.Nil(t, obj)
			require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
		})
	}
}

func TestPackfileRejectsInflatedObjectLargerThanDeclared(t *testing.T) {
	t.Parallel()

	pack, offsets := buildTestPack(t, testPackObject{
		typ:          plumbing.BlobObject,
		declaredSize: 1,
		content:      bytes.Repeat([]byte("a"), 1024),
	})
	file := writeTestPackFile(t, pack)
	entry := &idxfile.Entry{
		Hash:   testObjectHash(plumbing.BlobObject, []byte("a")),
		Offset: uint64(offsets[0]),
	}
	p := NewPackfile(file, WithIdx(&singleEntryIndex{entry: entry}))
	defer p.Close()

	iter, err := p.GetByType(plumbing.BlobObject)
	require.NoError(t, err)
	defer iter.Close()

	_, err = iter.Next()
	require.ErrorIs(t, err, ErrMalformedPackfile)
	require.ErrorContains(t, err, "inflated object exceeds declared size")
}

type singleEntryIndex struct {
	entry *idxfile.Entry
}

func (idx *singleEntryIndex) Contains(h plumbing.Hash) (bool, error) {
	return idx.entry.Hash.Equal(h), nil
}

func (idx *singleEntryIndex) FindOffset(h plumbing.Hash) (int64, error) {
	if idx.entry.Hash.Equal(h) {
		return int64(idx.entry.Offset), nil
	}
	return 0, plumbing.ErrObjectNotFound
}

func (idx *singleEntryIndex) FindCRC32(h plumbing.Hash) (uint32, error) {
	if idx.entry.Hash.Equal(h) {
		return idx.entry.CRC32, nil
	}
	return 0, plumbing.ErrObjectNotFound
}

func (idx *singleEntryIndex) FindHash(offset int64) (plumbing.Hash, error) {
	if idx.entry.Offset == uint64(offset) {
		return idx.entry.Hash, nil
	}
	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

func (idx *singleEntryIndex) Count() (int64, error) {
	return 1, nil
}

func (idx *singleEntryIndex) Entries() (idxfile.EntryIter, error) {
	return &singleEntryIter{entry: idx.entry}, nil
}

func (idx *singleEntryIndex) EntriesByOffset() (idxfile.EntryIter, error) {
	return &singleEntryIter{entry: idx.entry}, nil
}

type singleEntryIter struct {
	entry *idxfile.Entry
	done  bool
}

func (iter *singleEntryIter) Next() (*idxfile.Entry, error) {
	if iter.done {
		return nil, io.EOF
	}
	iter.done = true
	return iter.entry, nil
}

func (iter *singleEntryIter) Close() error {
	iter.done = true
	return nil
}
