package idxfile_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func sha256Idx(t *testing.T) []byte {
	t.Helper()
	f, err := fixtures.ByTag("packfile").ByObjectFormat("sha256").One().Idx()
	require.NoError(t, err)
	raw, err := io.ReadAll(f)
	require.NoError(t, err)
	return raw
}

func TestDecodeSHA256(t *testing.T) {
	t.Parallel()

	raw := sha256Idx(t)
	idx := idxfile.NewMemoryIndex(hash.SHA256)
	require.NoError(t, idxfile.NewDecoder(idxfile.FromBytes(raw), idxfile.WithObjectFormat(hash.SHA256)).Decode(idx))

	assert.Equal(t, hash.SHA256, idx.ObjectFormat())
	assert.Equal(t, hash.SHA256, idx.PackfileChecksum.Format())
	assert.Equal(t, hash.SHA256, idx.IdxChecksum.Format())

	iter, err := idx.Entries()
	require.NoError(t, err)
	e, err := iter.Next()
	require.NoError(t, err)
	assert.Equal(t, hash.SHA256, e.Hash.Format())
	require.NoError(t, iter.Close())

	var buf bytes.Buffer
	require.NoError(t, idxfile.Encode(&buf, idx))
	assert.Equal(t, raw, buf.Bytes())
}

func TestDecoderRejectsObjectFormatMismatch(t *testing.T) {
	t.Parallel()

	raw := sha256Idx(t)
	err := idxfile.NewDecoder(idxfile.FromBytes(raw), idxfile.WithObjectFormat(hash.SHA256)).
		Decode(idxfile.NewMemoryIndex(hash.SHA1))
	require.ErrorContains(t, err, "object format")

	err = idxfile.NewDecoder(idxfile.FromBytes(raw)).Decode(idxfile.NewMemoryIndex(hash.SHA256))
	require.ErrorContains(t, err, "object format")
}

func TestDecoderRejectsUnsupportedObjectFormat(t *testing.T) {
	t.Parallel()

	err := idxfile.NewDecoder(idxfile.FromBytes(sha256Idx(t)), idxfile.WithObjectFormat("md5")).
		Decode(idxfile.NewMemoryIndex("md5"))
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
}

func TestEncodeRejectsUnsupportedObjectFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := idxfile.Encode(&buf, idxfile.NewMemoryIndex("md5"))
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	assert.Zero(t, buf.Len())
}

func TestMemoryIndexObjectFormatDefaultsToSHA1(t *testing.T) {
	t.Parallel()

	assert.Equal(t, hash.SHA1, new(idxfile.MemoryIndex).ObjectFormat())
	assert.Equal(t, hash.SHA1, idxfile.NewMemoryIndex(hash.UnsetObjectFormat).ObjectFormat())
}

func TestWriterSHA256RoundTrip(t *testing.T) {
	t.Parallel()

	id := func(b byte) plumbing.Hash {
		h, ok := plumbing.FromHex(strings.Repeat(fmt.Sprintf("%02x", b), hash.SHA256Size))
		require.True(t, ok)
		return h
	}

	w := new(idxfile.Writer)
	require.NoError(t, w.OnHeader(3))
	require.NoError(t, w.OnInflatedObjectContent(id(0x30), 12, 3, nil))
	require.NoError(t, w.OnInflatedObjectContent(id(0x10), 40, 1, nil))
	require.NoError(t, w.OnInflatedObjectContent(id(0x20), 90, 2, nil))
	require.NoError(t, w.OnFooter(id(0xff)))
	written, err := w.Index()
	require.NoError(t, err)
	assert.Equal(t, hash.SHA256, written.ObjectFormat())

	var buf bytes.Buffer
	require.NoError(t, idxfile.Encode(&buf, written))

	idx := idxfile.NewMemoryIndex(hash.SHA256)
	require.NoError(t, idxfile.NewDecoder(idxfile.FromBytes(buf.Bytes()), idxfile.WithObjectFormat(hash.SHA256)).Decode(idx))
	assert.Equal(t, id(0xff), idx.PackfileChecksum)
	for _, want := range []struct {
		b      byte
		offset int64
	}{{0x10, 40}, {0x20, 90}, {0x30, 12}} {
		off, err := idx.FindOffset(id(want.b))
		require.NoError(t, err)
		assert.Equal(t, want.offset, off)
		got, err := idx.FindHash(want.offset)
		require.NoError(t, err)
		assert.Equal(t, id(want.b), got)
	}
}
