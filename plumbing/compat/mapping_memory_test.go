package compat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestMemoryMapping(t *testing.T) {
	t.Parallel()

	testHashMapping(t, func() HashMapping {
		return NewMemoryMapping()
	})
}

// testHashMapping is a shared test suite for all HashMapping implementations.
func testHashMapping(t *testing.T, newMapping func() HashMapping) {
	t.Helper()

	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddd")

	t.Run("empty mapping returns not found", func(t *testing.T) {
		t.Parallel()

		m := newMapping()
		_, err := m.NativeToCompat(native1)
		assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
		_, err = m.CompatToNative(compat1)
		assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
		count, err := m.Count()
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("add and lookup", func(t *testing.T) {
		t.Parallel()

		m := newMapping()
		require.NoError(t, m.Add(native1, compat1))

		got, err := m.NativeToCompat(native1)
		require.NoError(t, err)
		assert.True(t, got.Equal(compat1))

		got, err = m.CompatToNative(compat1)
		require.NoError(t, err)
		assert.True(t, got.Equal(native1))

		count, err := m.Count()
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("multiple mappings", func(t *testing.T) {
		t.Parallel()

		m := newMapping()
		require.NoError(t, m.Add(native1, compat1))
		require.NoError(t, m.Add(native2, compat2))

		count, err := m.Count()
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		got, err := m.NativeToCompat(native2)
		require.NoError(t, err)
		assert.True(t, got.Equal(compat2))
	})

	t.Run("overwrite mapping", func(t *testing.T) {
		t.Parallel()

		m := newMapping()
		require.NoError(t, m.Add(native1, compat1))
		require.NoError(t, m.Add(native1, compat2))

		got, err := m.NativeToCompat(native1)
		require.NoError(t, err)
		assert.True(t, got.Equal(compat2))

		_, err = m.CompatToNative(compat1)
		assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

		got, err = m.CompatToNative(compat2)
		require.NoError(t, err)
		assert.True(t, got.Equal(native1))
	})
}
