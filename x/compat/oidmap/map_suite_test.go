package oidmap

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
)

type countingMap interface {
	Map
	Count() (int, error)
}

type MapSuite struct {
	suite.Suite
	newMap func() countingMap
}

func (s *MapSuite) TestEmptyMappingReturnsNotFound() {
	m := s.newMap()

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	_, err := m.ToCompat(native)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
	_, err = m.ToNative(compat)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)

	count, err := m.Count()
	s.Require().NoError(err)
	s.Equal(0, count)
}

func (s *MapSuite) TestAddAndLookup() {
	m := s.newMap()

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	s.Require().NoError(m.Add(native, compat))

	got, err := m.ToCompat(native)
	s.Require().NoError(err)
	s.True(got.Equal(compat))

	got, err = m.ToNative(compat)
	s.Require().NoError(err)
	s.True(got.Equal(native))

	count, err := m.Count()
	s.Require().NoError(err)
	s.Equal(1, count)
}

func (s *MapSuite) TestMultipleMappings() {
	m := s.newMap()

	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd")

	s.Require().NoError(m.Add(native1, compat1))
	s.Require().NoError(m.Add(native2, compat2))

	count, err := m.Count()
	s.Require().NoError(err)
	s.Equal(2, count)

	got, err := m.ToCompat(native2)
	s.Require().NoError(err)
	s.True(got.Equal(compat2))
}

func (s *MapSuite) TestOverwriteNative() {
	m := s.newMap()

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd")

	s.Require().NoError(m.Add(native, compat1))
	s.Require().NoError(m.Add(native, compat2))

	got, err := m.ToCompat(native)
	s.Require().NoError(err)
	s.True(got.Equal(compat2))

	_, err = m.ToNative(compat1)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)

	got, err = m.ToNative(compat2)
	s.Require().NoError(err)
	s.True(got.Equal(native))

	count, err := m.Count()
	s.Require().NoError(err)
	s.Equal(1, count)
}

func (s *MapSuite) TestReassignCompatToDifferentNative() {
	m := s.newMap()

	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	s.Require().NoError(m.Add(native1, compat))
	s.Require().NoError(m.Add(native2, compat))

	_, err := m.ToCompat(native1)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)

	got, err := m.ToCompat(native2)
	s.Require().NoError(err)
	s.True(got.Equal(compat))

	got, err = m.ToNative(compat)
	s.Require().NoError(err)
	s.True(got.Equal(native2))

	count, err := m.Count()
	s.Require().NoError(err)
	s.Equal(1, count)
}

func TestMemoryMapSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, &MapSuite{
		newMap: func() countingMap { return NewMemory() },
	})
}

func TestFileLegacyMapSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, &MapSuite{
		newMap: func() countingMap {
			fs := memfs.New()
			_ = fs.MkdirAll("objects", 0o755)
			return NewFile(fs, "objects")
		},
	})
}

func TestFileObjectMapSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, &MapSuite{
		newMap: func() countingMap {
			fs := memfs.New()
			_ = fs.MkdirAll("objects", 0o755)
			return NewFileWithWriteMode(fs, "objects", FileWriteObjectMap)
		},
	})
}
