package git

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/storage/memory"
)

type StatusSuite struct {
	suite.Suite
	BaseSuite
	Files    *[]string
	Expected *map[string]bool
}

func TestStatusSuite(t *testing.T) {
	suite.Run(t, new(StatusSuite))
}

func (s *StatusSuite) SetupTest() {
	files := []string{
		filepath.Join("a", "a"),
		filepath.Join("b", "a"),
		filepath.Join("c", "b", "a"),
		filepath.Join("d", "b", "a"),
		filepath.Join("e", "b", "c", "a"),
	}
	s.Files = &files

	expected := map[string]bool{}
	for _, file := range files {
		expected[file] = true
	}
	s.Expected = &expected
}

func (s *StatusSuite) TestStatusReturnsFullPaths() {
	tests := []struct {
		name     string
		doChange bool
		strategy StatusStrategy
	}{
		{
			name:     "strategy:Empty with changes",
			doChange: true,
			strategy: Empty,
		},
		{
			name:     "strategy:Preload with changes",
			doChange: true,
			strategy: Preload,
		},
		{
			name:     "strategy:Preload without changes",
			doChange: false,
			strategy: Preload,
		},
	}

	for _, tv := range tests {
		s.Run(tv.name, func() {
			r, err := Init(memory.NewStorage(), memfs.New())
			s.NoError(err)

			w, err := r.Worktree()
			s.NoError(err)

			for _, fname := range *s.Files {
				file, err := w.Filesystem.Create(fname)
				s.NoError(err)

				_, err = file.Write([]byte("foo"))
				s.NoError(err)
				file.Close()

				_, err = w.Add(file.Name())
				s.NoError(err)
			}

			_, err = w.Commit("foo", &CommitOptions{All: true})
			s.NoError(err)

			partialExpected := map[string]bool{}
			if tv.doChange {
				for _, fname := range (*s.Files)[:len(*s.Files)-2] {
					file, err := w.Filesystem.Create(fname)
					if !s.NoError(err) {
						return
					}

					_, err = file.Write([]byte("fooo"))
					if !s.NoError(err) {
						return
					}
					file.Close()

					partialExpected[fname] = true
				}
			}

			status, err := w.StatusWithOptions(
				StatusOptions{
					Strategy: tv.strategy,
				},
			)
			s.NoError(err)

			exists := map[string]bool{}
			for file := range status {
				exists[file] = true
			}

			expected := s.Expected
			if tv.strategy == Empty {
				expected = &partialExpected
			}

			for file := range *expected {
				_, ok := exists[file]
				s.Truef(ok, "unexpected not ok for %s", file)
			}
		})
	}
}
