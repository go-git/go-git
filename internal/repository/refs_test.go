package repository

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

type packedObjectStorerStub struct {
	hashes []plumbing.Hash
}

func (s packedObjectStorerStub) ObjectPacks() ([]plumbing.Hash, error) {
	return s.hashes, nil
}

func (packedObjectStorerStub) DeleteOldObjectPackAndIndex(plumbing.Hash, time.Time) error {
	return nil
}

type namedPackedObjectStorerStub struct {
	packedObjectStorerStub
	names []string
}

func (s namedPackedObjectStorerStub) ObjectPackNames() ([]string, error) {
	return s.names, nil
}

func TestWriteObjectsInfoPacksUsesPhysicalNames(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	storer := namedPackedObjectStorerStub{
		packedObjectStorerStub: packedObjectStorerStub{hashes: []plumbing.Hash{hash}},
		names: []string{
			fmt.Sprintf("loose-%s.pack", hash),
			fmt.Sprintf("pack-%s.pack", hash),
		},
	}

	var got bytes.Buffer
	require.NoError(t, WriteObjectsInfoPacks(&got, storer))
	require.Equal(t,
		fmt.Sprintf("P loose-%[1]s.pack\nP pack-%[1]s.pack\n\n", hash),
		got.String(),
	)
}

func TestWriteObjectsInfoPacksUsesCanonicalFallback(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("2222222222222222222222222222222222222222")
	storer := packedObjectStorerStub{hashes: []plumbing.Hash{hash}}

	var got bytes.Buffer
	require.NoError(t, WriteObjectsInfoPacks(&got, storer))
	require.Equal(t, fmt.Sprintf("P pack-%s.pack\n\n", hash), got.String())
}
