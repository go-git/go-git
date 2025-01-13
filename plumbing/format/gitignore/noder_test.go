package gitignore_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type mockNoder struct {
	hash        []byte
	string      string
	name        string
	isDir       bool
	children    []noder.Noder
	childrenErr error
	skip        bool
}

func (m mockNoder) Hash() []byte                     { return m.hash }
func (m mockNoder) String() string                   { return m.string }
func (m mockNoder) Name() string                     { return m.name }
func (m mockNoder) IsDir() bool                      { return m.isDir }
func (m mockNoder) Children() ([]noder.Noder, error) { return m.children, m.childrenErr }
func (m mockNoder) NumChildren() (int, error)        { return len(m.children), m.childrenErr }
func (m mockNoder) Skip() bool                       { return m.skip }

func TestMatchNoder_Children(t *testing.T) {
	mock := mockNoder{
		name: ".",
		children: []noder.Noder{
			mockNoder{name: "volcano"},
			mockNoder{name: "caldera"},
			mockNoder{name: "super", isDir: true, children: []noder.Noder{
				mockNoder{name: "caldera", children: []noder.Noder{}},
			}},
		},
	}
	patterns := []gitignore.Pattern{
		gitignore.ParsePattern("**/middle/v[uo]l?ano", nil),
		gitignore.ParsePattern("volcano", nil),
	}

	tests := map[string]struct {
		Matcher     gitignore.Matcher
		Noder       mockNoder
		ExpErr      error
		ExpChildren []noder.Noder
		Skip        bool
	}{
		"children": {
			Matcher: gitignore.NewMatcher([]gitignore.Pattern{
				gitignore.ParsePattern("**/middle/v[uo]l?ano", nil),
				gitignore.ParsePattern("volcano", nil),
			}),
			Noder: mock,
			ExpChildren: []noder.Noder{
				mock.children[1],
				gitignore.IgnoreNoder(gitignore.NewMatcher(patterns), mock.children[2]),
			},
		},
		"error": {
			Matcher: gitignore.NewMatcher([]gitignore.Pattern{
				gitignore.ParsePattern("**/middle/v[uo]l?ano", nil),
				gitignore.ParsePattern("volcano", nil),
			}),
			Noder:  mockNoder{name: ".", childrenErr: fs.ErrNotExist},
			ExpErr: fs.ErrNotExist,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ignoreNoder := gitignore.IgnoreNoder(tc.Matcher, tc.Noder)

			children, err := ignoreNoder.Children()
			require.ErrorIs(t, err, tc.ExpErr)
			assert.Equal(t, tc.ExpChildren, children)

			// Do it twice for the cached children
			children, err = ignoreNoder.Children()
			require.ErrorIs(t, err, tc.ExpErr)
			assert.Equal(t, tc.ExpChildren, children)

			num, err := ignoreNoder.NumChildren()
			require.ErrorIs(t, err, tc.ExpErr)
			assert.Equal(t, len(tc.ExpChildren), num)
		})
	}
}

func TestMatchNoder_PathIgnored(t *testing.T) {
	matcher := gitignore.NewMatcher([]gitignore.Pattern{
		gitignore.ParsePattern("**/middle/v[uo]l?ano", nil),
		gitignore.ParsePattern("volcano", nil),
	})

	found := gitignore.IgnoreNoder(matcher, mockNoder{name: "."}).PathIgnored([]noder.Noder{
		mockNoder{name: "head"},
		mockNoder{name: "middle"},
		mockNoder{name: "volcano"},
	})
	assert.True(t, found)

	found = gitignore.IgnoreNoder(matcher, mockNoder{name: "."}).PathIgnored([]noder.Noder{
		mockNoder{name: "head"},
		mockNoder{name: "middle"},
		mockNoder{name: "potato"},
	})
	assert.False(t, found)
}

func TestMatchNoder_FindPath(t *testing.T) {
	mock := mockNoder{
		name: ".",
		children: []noder.Noder{
			mockNoder{name: "volcano"},
			mockNoder{name: "super", isDir: true, children: []noder.Noder{
				mockNoder{name: "volcano", children: []noder.Noder{}},
			}},
		},
	}
	matcher := gitignore.NewMatcher([]gitignore.Pattern{
		gitignore.ParsePattern("**/middle/v[uo]l?ano", nil),
		gitignore.ParsePattern("volcano", nil),
	})

	node, found := gitignore.IgnoreNoder(matcher, mock).FindPath([]noder.Noder{
		mockNoder{name: "super"},
		mockNoder{name: "volcano"},
	})
	assert.True(t, found)
	assert.Equal(t, noder.Path{mock.children[1], mock.children[1].(mockNoder).children[0]}, node)

	node, found = gitignore.IgnoreNoder(matcher, mock).FindPath([]noder.Noder{
		mockNoder{name: "super"},
		mockNoder{name: "caldera"},
	})
	assert.False(t, found)
	assert.Nil(t, node)
}
