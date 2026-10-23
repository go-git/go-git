package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
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

// TestFetchV2ReadsResponseAfterPack checks that a v2 fetch reads the packfile
// section to its closing flush-pkt. Storage that parses the pack stops
// reading at its trailer, so anything the server sends after it has to be
// read by the fetch: a band-3 error must fail it, and trailing progress must
// reach the caller.
func TestFetchV2ReadsResponseAfterPack(t *testing.T) {
	t.Parallel()

	storers := map[string]func(*testing.T) storage.Storer{
		"filesystem": func(t *testing.T) storage.Storer {
			st := filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault())
			t.Cleanup(func() { _ = st.Close() })
			return st
		},
		"memory": func(*testing.T) storage.Storer { return memory.NewStorage() },
	}

	for name, newStorer := range storers {
		for _, filter := range []packp.Filter{"", packp.FilterBlobNone()} {
			t.Run(name+"/error after pack/filter="+string(filter), func(t *testing.T) {
				t.Parallel()

				err := fetchV2Response(newStorer(t), filter, io.Discard,
					packfileSection(t, sideband.ErrorMessage, "remote boom"))
				require.Error(t, err)
				assert.Contains(t, err.Error(), "remote boom")
			})

			t.Run(name+"/progress after pack/filter="+string(filter), func(t *testing.T) {
				t.Parallel()

				var progress bytes.Buffer
				err := fetchV2Response(newStorer(t), filter, &progress,
					packfileSection(t, sideband.ProgressMessage, "Total 31 (delta 0)\n"))
				require.NoError(t, err)
				assert.Contains(t, progress.String(), "Total 31 (delta 0)")
			})
		}
	}
}

// fetchV2Response runs FetchV2 into st with a single round whose response is
// the packfile section in resp.
func fetchV2Response(st storage.Storer, filter packp.Filter, progress io.Writer, resp io.Reader) error {
	round := func(_ *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		return &packp.FetchOutput{Packfile: true}, resp, nil
	}
	req := &FetchRequest{
		Wants:    []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Progress: progress,
		Filter:   filter,
	}
	return FetchV2(context.Background(), st, req, round)
}
