package reference

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

const (
	hashA = "e8d3ffab552895c19b9fcf7aa264d277cde33881"
	hashB = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
)

type sliceSource struct {
	refs   []*plumbing.Reference
	err    error
	closed bool
}

func (s *sliceSource) Next() (*plumbing.Reference, error) {
	if len(s.refs) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	ref := s.refs[0]
	s.refs = s.refs[1:]
	return ref, nil
}

func (s *sliceSource) Close() { s.closed = true }

func refs(hash string, names ...string) []*plumbing.Reference {
	refs := make([]*plumbing.Reference, 0, len(names))
	for _, name := range names {
		refs = append(refs, plumbing.NewReferenceFromStrings(name, hash))
	}
	return refs
}

func TestOverlayIterFrontWinsAndRepeatsCollapse(t *testing.T) {
	t.Parallel()
	front := &sliceSource{refs: refs(hashA, "refs/heads/b", "refs/heads/d", "refs/heads/d")}
	back := &sliceSource{refs: refs(hashB, "refs/heads/a", "refs/heads/b", "refs/heads/c", "refs/heads/c", "refs/heads/d", "refs/heads/e")}

	var got []string
	require.NoError(t, NewOverlayIter(front, back).ForEach(func(r *plumbing.Reference) error {
		got = append(got, r.String())
		return nil
	}))

	assert.Equal(t, []string{
		hashB + " refs/heads/a",
		hashA + " refs/heads/b",
		hashB + " refs/heads/c",
		hashA + " refs/heads/d",
		hashB + " refs/heads/e",
	}, got)
	assert.True(t, front.closed)
	assert.True(t, back.closed)
}

func TestOverlayIterReportsSourceErrorsAndCloses(t *testing.T) {
	t.Parallel()
	errBroken := errors.New("broken")
	front := &sliceSource{refs: refs(hashA, "refs/heads/a")}
	back := &sliceSource{err: errBroken}

	iter := NewOverlayIter(front, back)
	_, err := iter.Next()
	require.ErrorIs(t, err, errBroken)

	iter.Close()
	assert.True(t, front.closed)
	assert.True(t, back.closed)
	_, err = iter.Next()
	assert.ErrorIs(t, err, io.EOF)
}

func TestOverlayIterForEachStop(t *testing.T) {
	t.Parallel()
	front := &sliceSource{refs: refs(hashA, "refs/heads/a", "refs/heads/b")}
	back := &sliceSource{}

	n := 0
	require.NoError(t, NewOverlayIter(front, back).ForEach(func(*plumbing.Reference) error {
		n++
		return storer.ErrStop
	}))
	assert.Equal(t, 1, n)
	assert.True(t, front.closed)
}
