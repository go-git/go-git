package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
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

func TestUpdateShallowMerge(t *testing.T) {
	t.Parallel()

	a := plumbing.NewHash("1111111111111111111111111111111111111111")
	b := plumbing.NewHash("2222222222222222222222222222222222222222")
	c := plumbing.NewHash("3333333333333333333333333333333333333333")
	d := plumbing.NewHash("4444444444444444444444444444444444444444")

	for _, tc := range []struct {
		name     string
		boundary []plumbing.Hash
		info     *packp.ShallowUpdate
		want     []plumbing.Hash
	}{
		{
			name:     "existing order kept, new shallows appended once, unshallows removed",
			boundary: []plumbing.Hash{a, b},
			info:     &packp.ShallowUpdate{Shallows: []plumbing.Hash{c, b, d, c}, Unshallows: []plumbing.Hash{a}},
			want:     []plumbing.Hash{b, c, d},
		},
		{
			name:     "unshallow removes every copy",
			boundary: []plumbing.Hash{a, b, a},
			info:     &packp.ShallowUpdate{Unshallows: []plumbing.Hash{a}},
			want:     []plumbing.Hash{b},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()
			require.NoError(t, st.SetShallow(tc.boundary))

			require.NoError(t, UpdateShallow(st, tc.info))

			shallows, err := st.Shallow()
			require.NoError(t, err)
			require.Equal(t, tc.want, shallows)
		})
	}
}

func BenchmarkUpdateShallow(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 50_000} {
		hashes := make([]plumbing.Hash, n)
		for i := range hashes {
			hashes[i] = plumbing.NewHash(fmt.Sprintf("%040x", i+1))
		}

		b.Run("shallow/"+strconv.Itoa(n), func(b *testing.B) {
			info := &packp.ShallowUpdate{Shallows: hashes}
			for b.Loop() {
				if err := UpdateShallow(memory.NewStorage(), info); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("unshallow/"+strconv.Itoa(n), func(b *testing.B) {
			info := &packp.ShallowUpdate{Unshallows: hashes}
			for b.Loop() {
				b.StopTimer()
				st := memory.NewStorage()
				if err := st.SetShallow(hashes); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				if err := UpdateShallow(st, info); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
