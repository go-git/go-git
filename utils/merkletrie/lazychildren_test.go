package merkletrie_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

var errUnreadable = errors.New("children cannot be read")

// stubNoder is a noder whose children are fixed, or, when err is set, whose
// children cannot be read at all, like a tree object absent from storage.
type stubNoder struct {
	name     string
	hash     string
	dir      bool
	children []noder.Noder
	err      error
}

func file(name, hash string) *stubNoder { return &stubNoder{name: name, hash: hash} }

func dir(name, hash string, children ...noder.Noder) *stubNoder {
	return &stubNoder{name: name, hash: hash, dir: true, children: children}
}

func unreadableDir(name, hash string) *stubNoder {
	return &stubNoder{name: name, hash: hash, dir: true, err: errUnreadable}
}

func (n *stubNoder) Hash() []byte   { return []byte(n.hash) }
func (n *stubNoder) String() string { return n.name }
func (n *stubNoder) Name() string   { return n.name }
func (n *stubNoder) IsDir() bool    { return n.dir }
func (n *stubNoder) Skip() bool     { return false }

func (n *stubNoder) Children() ([]noder.Noder, error) {
	if n.err != nil {
		return nil, n.err
	}
	if n.children == nil {
		return noder.NoChildren, nil
	}
	return n.children, nil
}

func (n *stubNoder) NumChildren() (int, error) {
	children, err := n.Children()
	return len(children), err
}

func stubHashEqual(a, b noder.Hasher) bool { return bytes.Equal(a.Hash(), b.Hash()) }

func TestIterNextDoesNotReadChildren(t *testing.T) {
	t.Parallel()

	root := dir("", "root", file("a", "1"), unreadableDir("broken", "2"), file("z", "3"))
	iter, err := merkletrie.NewIter(root)
	require.NoError(t, err)

	var names []string
	for range 10 {
		path, err := iter.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, path.String())
	}
	assert.Equal(t, []string{"a", "broken", "z"}, names)
}

func TestIterStepReturnsChildrenError(t *testing.T) {
	t.Parallel()

	root := dir("", "root", file("a", "1"), unreadableDir("broken", "2"), file("z", "3"))
	iter, err := merkletrie.NewIter(root)
	require.NoError(t, err)

	for _, want := range []string{"a", "broken"} {
		path, err := iter.Step()
		require.NoError(t, err)
		require.Equal(t, want, path.String())
	}

	_, err = iter.Step()
	assert.ErrorIs(t, err, errUnreadable)
}

func TestDiffTreeDoesNotReadUnchangedDirs(t *testing.T) {
	t.Parallel()

	// The iterator sorts a node's children in place, so each subtest builds
	// its own trees.
	from := func() noder.Noder {
		return dir("", "root1", file("a", "1"), unreadableDir("broken", "2"), file("z", "3"))
	}
	to := func() noder.Noder {
		return dir("", "root2", file("a", "1b"), unreadableDir("broken", "2"), file("z", "3b"))
	}

	t.Run("identical trees", func(t *testing.T) {
		t.Parallel()

		changes, err := merkletrie.DiffTree(from(), from(), stubHashEqual)
		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("siblings modified", func(t *testing.T) {
		t.Parallel()

		changes, err := merkletrie.DiffTree(from(), to(), stubHashEqual)
		require.NoError(t, err)

		got := make([]string, 0, len(changes))
		for _, c := range changes {
			action, err := c.Action()
			require.NoError(t, err)
			got = append(got, action.String()+" "+c.To.String())
		}
		assert.Equal(t, []string{"Modify a", "Modify z"}, got)
	})
}

func TestDiffTreeReturnsChildrenErrorOfChangedDirs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		from, to noder.Noder
	}{
		{
			"from side unreadable",
			dir("", "root1", unreadableDir("d", "2")),
			dir("", "root2", dir("d", "2b", file("f", "1"))),
		},
		{
			"to side unreadable",
			dir("", "root1", dir("d", "2", file("f", "1"))),
			dir("", "root2", unreadableDir("d", "2b")),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := merkletrie.DiffTree(tc.from, tc.to, stubHashEqual)
			assert.ErrorIs(t, err, errUnreadable)
		})
	}
}
