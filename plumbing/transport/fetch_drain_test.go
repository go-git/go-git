package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

// packfileSection muxes a complete packfile on band 1, then writes trailer
// (band-2 progress or a band-3 error) and the closing flush-pkt. Reading
// past the flush-pkt fails.
func packfileSection(t *testing.T, ch sideband.Channel, trailer string) io.Reader {
	t.Helper()

	pf, err := fixtures.Basic().One().Packfile()
	require.NoError(t, err)
	pack, err := io.ReadAll(pf)
	require.NoError(t, err)

	var buf bytes.Buffer
	mux := sideband.NewMuxer(sideband.Sideband64k, &buf)
	_, err = mux.Write(pack)
	require.NoError(t, err)
	_, err = mux.WriteChannel(ch, []byte(trailer))
	require.NoError(t, err)
	require.NoError(t, pktline.WriteFlush(&buf))

	return io.MultiReader(&buf, iotest.ErrReader(errors.New("read past the closing flush-pkt")))
}

// TestFetchPackReadsResponseAfterPack checks that FetchPack reads the sideband
// stream to its closing flush-pkt after the packfile. Storage that parses the
// pack stops reading at its trailer, so anything the server sends after it
// has to be read by FetchPack: a band-3 error must fail the fetch, and
// trailing progress must reach the caller.
func TestFetchPackReadsResponseAfterPack(t *testing.T) {
	t.Parallel()

	caps := capability.List{}
	caps.Set(capability.Sideband64k)

	storers := map[string]func(*testing.T) storage.Storer{
		"filesystem": func(t *testing.T) storage.Storer {
			st, _ := emptyRepoStorage(t)
			return st
		},
		"memory": func(*testing.T) storage.Storer { return memory.NewStorage() },
	}

	for name, newStorer := range storers {
		for _, filter := range []packp.Filter{"", packp.FilterBlobNone()} {
			t.Run(name+"/error after pack/filter="+string(filter), func(t *testing.T) {
				t.Parallel()

				resp := packfileSection(t, sideband.ErrorMessage, "remote boom")
				err := FetchPack(context.Background(), newStorer(t), caps, io.NopCloser(resp), nil,
					&FetchRequest{Progress: io.Discard, Filter: filter})
				require.Error(t, err)
				assert.Contains(t, err.Error(), "remote boom")
			})

			t.Run(name+"/progress after pack/filter="+string(filter), func(t *testing.T) {
				t.Parallel()

				resp := packfileSection(t, sideband.ProgressMessage, "Total 31 (delta 0)\n")
				var progress bytes.Buffer
				err := FetchPack(context.Background(), newStorer(t), caps, io.NopCloser(resp), nil,
					&FetchRequest{Progress: &progress, Filter: filter})
				require.NoError(t, err)
				assert.Contains(t, progress.String(), "Total 31 (delta 0)")
			})
		}
	}
}
