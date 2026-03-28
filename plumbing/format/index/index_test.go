package index

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexAdd(t *testing.T) {
	t.Parallel()
	idx := &Index{}
	e := idx.Add("foo")
	e.Size = 42

	e, err := idx.Entry("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)
	assert.Equal(t, uint32(42), e.Size)
}

func TestIndexEntry(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Entry("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)

	e, err = idx.Entry("missing")
	assert.Nil(t, e)
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

func TestIndexRemove(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Remove("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)

	e, err = idx.Remove("foo")
	assert.Nil(t, e)
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

func TestIndexGlob(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo/bar/bar", Size: 42},
			{Name: "foo/baz/qux", Size: 42},
			{Name: "fux", Size: 82},
		},
	}

	m, err := idx.Glob(filepath.Join("foo", "b*"))
	require.NoError(t, err)
	assert.Len(t, m, 2)
	assert.Equal(t, "foo/bar/bar", m[0].Name)
	assert.Equal(t, "foo/baz/qux", m[1].Name)

	m, err = idx.Glob("f*")
	require.NoError(t, err)
	assert.Len(t, m, 3)

	m, err = idx.Glob("f*/baz/q*")
	require.NoError(t, err)
	assert.Len(t, m, 1)
}
