//go:build wasip1

package git

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/storage/memory"
)

func TestWasmInit(t *testing.T) {
	st := memory.NewStorage()
	wt := memfs.New()

	r, err := Init(st, WithWorkTree(wt))
	require.NoError(t, err)
	assert.NotNil(t, r)

	h := createCommit(t, r)
	assert.False(t, h.IsZero())

	ref, err := r.Head()
	require.NoError(t, err)
	require.NotNil(t, ref)
	assert.False(t, ref.Hash().IsZero())
	assert.Equal(t, h, ref.Hash())
}
