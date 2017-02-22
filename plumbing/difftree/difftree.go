package difftree

import (
	"bytes"

	"srcd.works/go-git.v4/plumbing/object"
	"srcd.works/go-git.v4/utils/merkletrie"
	"srcd.works/go-git.v4/utils/merkletrie/noder"
)

func DiffTree(a, b *object.Tree) ([]*Change, error) {
	from := newTreeNoder(a)
	to := newTreeNoder(b)

	merkletrieChanges, err := merkletrie.DiffTree(from, to, hashEqual)
	if err != nil {
		return nil, err
	}

	return newChanges(merkletrieChanges)
}

func hashEqual(a, b noder.Hasher) bool {
	return bytes.Equal(a.Hash(), b.Hash())
}
