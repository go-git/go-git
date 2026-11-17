package transport

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

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

// TestFetchPackRejectsShallowUpdateBeforePack checks that a shallow update of
// another object format fails the fetch before the pack is read, as Git dies
// on the shallow line before reading the pack, so no object is stored.
func TestFetchPackRejectsShallowUpdateBeforePack(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage(memory.WithObjectFormat(formatcfg.SHA256))
	body := &unreadBody{t: t}
	info := &packp.ShallowUpdate{
		Shallows: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
	}

	err := FetchPack(context.Background(), st, capability.List{}, body, info, &FetchRequest{})
	require.ErrorContains(t, err, "is not a sha256 object ID")
	assert.True(t, body.closed, "packf left open")

	shallows, err := st.Shallow()
	require.NoError(t, err)
	assert.Empty(t, shallows)
}
