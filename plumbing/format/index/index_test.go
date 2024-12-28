package index

import (
	"path/filepath"
)

func (s *IndexSuite) TestIndexAdd() {
	idx := &Index{}
	e := idx.Add("foo")
	e.Size = 42

	e, err := idx.Entry("foo")
	s.NoError(err)
	s.Equal("foo", e.Name)
	s.Equal(uint32(42), e.Size)
}

func (s *IndexSuite) TestIndexEntry() {
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Entry("foo")
	s.NoError(err)
	s.Equal("foo", e.Name)

	e, err = idx.Entry("missing")
	s.Nil(e)
	s.ErrorIs(err, ErrEntryNotFound)
}

func (s *IndexSuite) TestIndexRemove() {
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo", Size: 42},
			{Name: "bar", Size: 82},
		},
	}

	e, err := idx.Remove("foo")
	s.NoError(err)
	s.Equal("foo", e.Name)

	e, err = idx.Remove("foo")
	s.Nil(e)
	s.ErrorIs(err, ErrEntryNotFound)
}

func (s *IndexSuite) TestIndexGlob() {
	idx := &Index{
		Entries: []*Entry{
			{Name: "foo/bar/bar", Size: 42},
			{Name: "foo/baz/qux", Size: 42},
			{Name: "fux", Size: 82},
		},
	}

	m, err := idx.Glob(filepath.Join("foo", "b*"))
	s.NoError(err)
	s.Len(m, 2)
	s.Equal("foo/bar/bar", m[0].Name)
	s.Equal("foo/baz/qux", m[1].Name)

	m, err = idx.Glob("f*")
	s.NoError(err)
	s.Len(m, 3)

	m, err = idx.Glob("f*/baz/q*")
	s.NoError(err)
	s.Len(m, 1)
}
