package packfile_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

var (
	errInjectedClose = errors.New("injected close error")
	errInjectedSet   = errors.New("injected set error")
)

// rawlessStorer implements every storer.EncodedObjectStorer method but not
// storer.RawObjectWriter, as a third-party storer written before
// RawObjectWriter existed would.
type rawlessStorer struct {
	objects   map[plumbing.Hash]plumbing.EncodedObject
	failClose plumbing.Hash
	failSet   plumbing.Hash
}

var _ storer.EncodedObjectStorer = (*rawlessStorer)(nil)

func newRawlessStorer() *rawlessStorer {
	return &rawlessStorer{objects: make(map[plumbing.Hash]plumbing.EncodedObject)}
}

func (s *rawlessStorer) NewEncodedObject() plumbing.EncodedObject {
	return &failingCloseObject{MemoryObject: &plumbing.MemoryObject{}, failOn: s.failClose}
}

func (s *rawlessStorer) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	h := obj.Hash()
	if !s.failSet.IsZero() && h == s.failSet {
		return plumbing.ZeroHash, errInjectedSet
	}
	s.objects[h] = obj
	return h, nil
}

func (s *rawlessStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, ok := s.objects[h]
	if !ok || (t != plumbing.AnyObject && obj.Type() != t) {
		return nil, plumbing.ErrObjectNotFound
	}
	return obj, nil
}

func (s *rawlessStorer) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	var objs []plumbing.EncodedObject
	for _, obj := range s.objects {
		if t == plumbing.AnyObject || obj.Type() == t {
			objs = append(objs, obj)
		}
	}
	return storer.NewEncodedObjectSliceIter(objs), nil
}

func (s *rawlessStorer) HasEncodedObject(h plumbing.Hash) error {
	if _, ok := s.objects[h]; !ok {
		return plumbing.ErrObjectNotFound
	}
	return nil
}

func (s *rawlessStorer) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	obj, ok := s.objects[h]
	if !ok {
		return 0, plumbing.ErrObjectNotFound
	}
	return obj.Size(), nil
}

func (s *rawlessStorer) AddAlternate(string) error { return nil }

type failingCloseObject struct {
	*plumbing.MemoryObject
	failOn plumbing.Hash
}

func (o *failingCloseObject) Writer() (io.WriteCloser, error) {
	return o, nil
}

func (o *failingCloseObject) Close() error {
	if !o.failOn.IsZero() && o.Hash() == o.failOn {
		return errInjectedClose
	}
	return nil
}

func TestParserStoresIntoStorerWithoutRawObjectWriter(t *testing.T) {
	t.Parallel()

	pack, midHash, leafHash := buildRefOnOfsDeltaChainPack(t)
	baseHash := blobHash([]byte("a stable base payload used by the OFS-delta entry"))

	s := newRawlessStorer()
	_, ok := any(s).(storer.RawObjectWriter)
	require.False(t, ok)

	_, err := packfile.NewParser(bytes.NewReader(pack), packfile.WithStorage(s)).Parse()
	require.NoError(t, err)

	want := map[plumbing.Hash]string{
		baseHash: "a stable base payload used by the OFS-delta entry",
		midHash:  "a stable base payload modified by the OFS-delta entry",
		leafHash: "a stable base payload modified twice for the REF-delta",
	}
	require.Len(t, s.objects, len(want))
	for h, content := range want {
		obj, err := s.EncodedObject(plumbing.BlobObject, h)
		require.NoError(t, err, "object %s", h)

		r, err := obj.Reader()
		require.NoError(t, err)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		assert.Equal(t, content, string(got))
	}
}

func TestParserSurfacesFallbackWriterErrors(t *testing.T) {
	t.Parallel()

	pack, midHash, _ := buildRefOnOfsDeltaChainPack(t)
	baseHash := blobHash([]byte("a stable base payload used by the OFS-delta entry"))

	tests := []struct {
		name      string
		failClose plumbing.Hash
		failSet   plumbing.Hash
		wantErr   error
	}{
		{
			name:      "scanner path close",
			failClose: baseHash,
			wantErr:   errInjectedClose,
		},
		{
			name:    "scanner path set",
			failSet: baseHash,
			wantErr: errInjectedSet,
		},
		{
			name:      "parser path close",
			failClose: midHash,
			wantErr:   errInjectedClose,
		},
		{
			name:    "parser path set",
			failSet: midHash,
			wantErr: errInjectedSet,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newRawlessStorer()
			s.failClose = tc.failClose
			s.failSet = tc.failSet

			_, err := packfile.NewParser(bytes.NewReader(pack), packfile.WithStorage(s)).Parse()
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestParserFallbackStoresNothingForMalformedEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		declaredSize int64
		payload      []byte
		wantErr      error
	}{
		{
			name:         "inflates to fewer bytes than declared",
			declaredSize: 32,
			payload:      bytes.Repeat([]byte{'x'}, 8),
			wantErr:      packfile.ErrInflatedSizeShort,
		},
		{
			name:         "inflates to more bytes than declared",
			declaredSize: 8,
			payload:      bytes.Repeat([]byte{'x'}, 32),
			wantErr:      packfile.ErrInflatedSizeMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var body bytes.Buffer
			h := sha1.New()
			w := io.MultiWriter(&body, h)

			_, _ = w.Write([]byte("PACK"))
			_ = binary.Write(w, binary.BigEndian, uint32(2))
			_ = binary.Write(w, binary.BigEndian, uint32(1))

			writePackObjectHeader(t, w, plumbing.BlobObject, tc.declaredSize)
			writeZlibPayload(t, w, tc.payload)

			_, _ = body.Write(h.Sum(nil))

			s := newRawlessStorer()
			_, err := packfile.NewParser(bytes.NewReader(body.Bytes()), packfile.WithStorage(s)).Parse()
			require.ErrorIs(t, err, tc.wantErr)
			assert.Empty(t, s.objects)
		})
	}
}
