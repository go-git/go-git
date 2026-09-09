package rad

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

func fixtureStorer(t *testing.T) *memory.Storage {
	t.Helper()

	st := memory.NewStorage()
	commit := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	refs := []*plumbing.Reference{
		plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main"),
		plumbing.NewHashReference("refs/heads/main", commit),
		plumbing.NewHashReference("refs/tags/v1.0.0", commit),
		plumbing.NewHashReference("refs/rad/sigrefs", commit),
		plumbing.NewHashReference("refs/rad/id", commit),
		plumbing.NewHashReference("refs/namespaces/nid1/refs/heads/main", commit),
		plumbing.NewHashReference("refs/namespaces/nid1/refs/tags/v1.0.0", commit),
	}
	for _, ref := range refs {
		require.NoError(t, st.SetReference(ref))
	}
	return st
}

func TestCanonical_IterReferences(t *testing.T) {
	t.Parallel()

	base := fixtureStorer(t)
	c := newCanonical(base)

	it, err := c.IterReferences()
	require.NoError(t, err)

	var names []string
	for {
		ref, err := it.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, ref.Name().String())
	}
	it.Close()

	assert.ElementsMatch(t, []string{"HEAD", "refs/heads/main", "refs/tags/v1.0.0"}, names)
}

func TestCanonical_Reference(t *testing.T) {
	t.Parallel()

	base := fixtureStorer(t)
	c := newCanonical(base)

	ref, err := c.Reference(plumbing.HEAD)
	require.NoError(t, err)
	assert.Equal(t, plumbing.ReferenceName("refs/heads/main"), ref.Target())

	ref, err = c.Reference("refs/heads/main")
	require.NoError(t, err)
	assert.False(t, ref.Hash().IsZero())

	_, err = c.Reference("refs/rad/sigrefs")
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)

	_, err = c.Reference("refs/namespaces/nid1/refs/heads/main")
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestCanonical_ReadOnly(t *testing.T) {
	t.Parallel()

	c := newCanonical(fixtureStorer(t))

	err := c.SetReference(plumbing.NewHashReference("refs/heads/other", plumbing.ZeroHash))
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)

	err = c.CheckAndSetReference(plumbing.NewHashReference("refs/heads/other", plumbing.ZeroHash), nil)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)

	err = c.RemoveReference("refs/heads/main")
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}

func TestNamespaced_IterReferences(t *testing.T) {
	t.Parallel()

	base := fixtureStorer(t)
	n := newNamespaced(base, "nid1")

	it, err := n.IterReferences()
	require.NoError(t, err)

	var names []string
	var sawHead bool
	for {
		ref, err := it.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if ref.Name() == plumbing.HEAD {
			sawHead = true
		}
		names = append(names, ref.Name().String())
	}
	it.Close()

	assert.False(t, sawHead, "namespaced view must not advertise HEAD")
	assert.ElementsMatch(t, []string{"refs/heads/main", "refs/tags/v1.0.0"}, names)
}

func TestNamespaced_Reference(t *testing.T) {
	t.Parallel()

	base := fixtureStorer(t)
	n := newNamespaced(base, "nid1")

	ref, err := n.Reference("refs/heads/main")
	require.NoError(t, err)
	assert.False(t, ref.Hash().IsZero())
	assert.Equal(t, plumbing.ReferenceName("refs/heads/main"), ref.Name())

	_, err = n.Reference(plumbing.HEAD)
	assert.Error(t, err)
}

func TestNamespaced_ReadOnly(t *testing.T) {
	t.Parallel()

	n := newNamespaced(fixtureStorer(t), "nid1")

	err := n.SetReference(plumbing.NewHashReference("refs/heads/other", plumbing.ZeroHash))
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)

	err = n.RemoveReference("refs/heads/main")
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}

// closeCountingStorer records Close calls so a view can be checked to
// forward them to the storer it wraps.
type closeCountingStorer struct {
	*memory.Storage
	closed int
}

func (s *closeCountingStorer) Close() error {
	s.closed++
	return nil
}

func TestViewsForwardClose(t *testing.T) {
	t.Parallel()

	// The composed file transport releases its storer via an io.Closer type
	// assertion, and storage.Storer does not embed io.Closer. A view that
	// failed to satisfy the assertion would silently leak the underlying
	// *filesystem.Storage on every connection.
	for name, view := range map[string]func(storage.Storer) storage.Storer{
		"readOnly":   func(s storage.Storer) storage.Storer { return newReadOnly(s) },
		"canonical":  func(s storage.Storer) storage.Storer { return newCanonical(s) },
		"namespaced": func(s storage.Storer) storage.Storer { return newNamespaced(s, "nid1") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base := &closeCountingStorer{Storage: fixtureStorer(t)}

			closer, ok := view(base).(io.Closer)
			require.True(t, ok, "view must satisfy io.Closer")
			require.NoError(t, closer.Close())

			assert.Equal(t, 1, base.closed)
		})
	}
}

func TestViewClose_NonCloseableBaseSucceeds(t *testing.T) {
	t.Parallel()

	// memory.Storage has no Close of its own; the view still has to satisfy
	// the assertion and report success rather than panicking on the
	// type-assert-and-call.
	closer, ok := any(newCanonical(memory.NewStorage())).(io.Closer)
	require.True(t, ok, "view must satisfy io.Closer")

	assert.NoError(t, closer.Close())
}

func TestReadOnly_RejectsWrites(t *testing.T) {
	t.Parallel()

	// The AllRefs view goes through newReadOnly rather than the filtering
	// views, and must not become a writable path into Radicle storage.
	ro := newReadOnly(fixtureStorer(t))
	ref := plumbing.NewHashReference("refs/heads/pushed", plumbing.ZeroHash)

	assert.ErrorIs(t, ro.SetReference(ref), transport.ErrCommandUnsupported)
	assert.ErrorIs(t, ro.CheckAndSetReference(ref, nil), transport.ErrCommandUnsupported)
	assert.ErrorIs(t, ro.RemoveReference("refs/heads/main"), transport.ErrCommandUnsupported)
}
