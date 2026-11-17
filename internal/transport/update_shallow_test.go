package transport

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestCheckShallowUpdateRejectsOtherObjectFormat(t *testing.T) {
	t.Parallel()

	sha1 := plumbing.NewHash("1111111111111111111111111111111111111111")
	sha256 := plumbing.NewHash("2222222222222222222222222222222222222222222222222222222222222222")

	for _, tc := range []struct {
		name          string
		format        formatcfg.ObjectFormat
		have, foreign plumbing.Hash
	}{
		{name: "sha1 into sha256", format: formatcfg.SHA256, have: sha256, foreign: sha1},
		{name: "sha256 into sha1", format: formatcfg.SHA1, have: sha1, foreign: sha256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage(memory.WithObjectFormat(tc.format))
			for _, info := range []*packp.ShallowUpdate{
				{Shallows: []plumbing.Hash{tc.foreign}},
				{Unshallows: []plumbing.Hash{tc.foreign}},
			} {
				require.ErrorContains(t, CheckShallowUpdate(st, info), "is not a "+tc.format.String()+" object ID")
			}

			require.NoError(t, CheckShallowUpdate(st, &packp.ShallowUpdate{
				Shallows:   []plumbing.Hash{tc.have},
				Unshallows: []plumbing.Hash{tc.have},
			}))
		})
	}
}

var errConfig = errors.New("config unreadable")

// configErrStorer fails every read of its config.
type configErrStorer struct {
	*memory.Storage
}

func (configErrStorer) Config() (*config.Config, error) {
	return nil, errConfig
}

// TestCheckShallowUpdateReturnsConfigError checks that an unreadable config
// fails the check, rather than being taken for a SHA-1 repository.
func TestCheckShallowUpdateReturnsConfigError(t *testing.T) {
	t.Parallel()

	st := configErrStorer{memory.NewStorage(memory.WithObjectFormat(formatcfg.SHA256))}
	err := CheckShallowUpdate(st, &packp.ShallowUpdate{
		Shallows: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
	})
	require.ErrorIs(t, err, errConfig)
}

// unreadBody fails the test if it is read, and records whether it was closed.
type unreadBody struct {
	t      *testing.T
	closed bool
}

func (b *unreadBody) Read([]byte) (int, error) {
	b.t.Error("pack read after the shallow update was rejected")
	return 0, io.EOF
}

func (b *unreadBody) Close() error {
	b.closed = true
	return nil
}

// TestFetchV2RejectsShallowUpdateBeforePack checks that a shallow update of
// another object format fails the fetch before the pack is read, as Git dies
// on the shallow line before reading the pack, so no object is stored.
func TestFetchV2RejectsShallowUpdateBeforePack(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage(memory.WithObjectFormat(formatcfg.SHA256))
	body := &unreadBody{t: t}
	round := func(*packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		return &packp.FetchOutput{
			ShallowInfo: &packp.ShallowInfo{
				Shallows: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
			},
			Packfile: true,
		}, body, nil
	}
	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("2222222222222222222222222222222222222222222222222222222222222222")},
		Depth: 1,
	}

	err := FetchV2(context.Background(), st, req, round)
	require.ErrorContains(t, err, "is not a sha256 object ID")
	assert.True(t, body.closed, "pack response left open")

	shallows, err := st.Shallow()
	require.NoError(t, err)
	assert.Empty(t, shallows)
}
