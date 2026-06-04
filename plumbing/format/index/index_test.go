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
	e, err := idx.Add("foo")
	require.NoError(t, err)
	e.Size = 42

	e, err = idx.Entry("foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", e.Name)
	assert.Equal(t, uint32(42), e.Size)
}

func TestIndexAddRejectsDangerousPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{".git at root", ".git/config"},
		{"final-component .git", "submodule/.git"},
		{"git~1 short name", "git~1/HEAD"},
		{"NTFS trailing space on .git", ".git /config"},
		{"NTFS trailing dot on .git", ".git./config"},
		{"NTFS alternate data stream", ".git::$INDEX_ALLOCATION/config"},
		{"NTFS trailing space on git~1", "git~1 /HEAD"},
		{"NTFS alternate data stream on git~1", "git~1::$DATA/HEAD"},
		{"HFS+ zero-width character in .git", ".g\u200cit/config"},
		{"dot-dot traversal", "a/../../etc/passwd"},
		{"single dot component", "a/./b"},
		{"control character", "foo\x01bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idx := &Index{}
			e, err := idx.Add(tc.path)
			assert.Nil(t, e, "Add should not return an entry for %q", tc.path)
			require.Error(t, err)
			assert.Empty(t, idx.Entries, "Add should not record %q", tc.path)
		})
	}
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
