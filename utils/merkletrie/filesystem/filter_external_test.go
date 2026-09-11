package filesystem_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	f "github.com/go-git/go-git/v6/utils/merkletrie/filesystem"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

func TestOptionsComparable(t *testing.T) {
	t.Parallel()
	a, b := f.Options{AutoCRLF: true}, f.Options{AutoCRLF: true}
	require.True(t, a == b)
	require.Equal(t, "typed", map[f.Options]string{a: "typed"}[b])
	require.Equal(t, "dynamic", map[any]string{a: "dynamic"}[b])
}

func TestRootNodeDotGitDefaults(t *testing.T) {
	t.Parallel()
	for _, directory := range []bool{false, true} {
		for _, constructor := range []string{"legacy", "options", "nil-filter"} {
			t.Run(fmt.Sprintf("%s/directory=%t", constructor, directory), func(t *testing.T) {
				t.Parallel()
				fs := memfs.New()
				name := ".GIT"
				if directory {
					name += "/inner.txt"
				}
				require.NoError(t, util.WriteFile(fs, name, []byte("visible"), 0o644))
				require.NoError(t, util.WriteFile(fs, ".git/HEAD", []byte("hidden"), 0o644))
				var root noder.Noder
				switch constructor {
				case "legacy":
					root = f.NewRootNode(fs, nil)
				case "options":
					root = f.NewRootNodeWithOptions(fs, nil, f.Options{})
				case "nil-filter":
					root = f.NewRootNodeWithDotGitFilter(fs, nil, f.Options{}, nil)
				}
				children, err := root.Children()
				require.NoError(t, err)
				require.Len(t, children, 1)
				require.Equal(t, ".GIT", children[0].Name())
				if directory {
					nested, err := children[0].Children()
					require.NoError(t, err)
					require.Len(t, nested, 1)
					require.Equal(t, "inner.txt", nested[0].Name())
				}
			})
		}
	}
}

func TestRootNodeDotGitFilterPropagates(t *testing.T) {
	t.Parallel()
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			t.Parallel()
			fs := memfs.New()
			for _, parent := range []string{"", "sub/", "sub/deep/"} {
				for _, name := range []string{".git", ".GIT", "git~1"} {
					p := parent + name
					if directory {
						p += "/inner.txt"
					}
					require.NoError(t, util.WriteFile(fs, p, []byte("hidden"), 0o644))
				}
			}
			require.NoError(t, util.WriteFile(fs, "sub/deep/visible", []byte("visible"), 0o644))
			var visited []string
			root := f.NewRootNodeWithDotGitFilter(fs, nil, f.Options{}, func(name string) bool {
				visited = append(visited, name)
				return strings.EqualFold(name, ".git") || name == "git~1"
			})
			for _, want := range []string{"sub", "deep", "visible"} {
				children, err := root.Children()
				require.NoError(t, err)
				require.Len(t, children, 1)
				require.Equal(t, want, children[0].Name())
				root = children[0]
			}
			for _, name := range []string{".git", ".GIT", "git~1"} {
				count := 0
				for _, got := range visited {
					if got == name {
						count++
					}
				}
				require.Equal(t, 3, count, name)
			}
		})
	}
}
