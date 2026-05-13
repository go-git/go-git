package oidmap

import (
	"bytes"
	"slices"

	"github.com/go-git/go-git/v6/plumbing"
)

type mapPair struct {
	native plumbing.Hash
	compat plumbing.Hash
}

func sortedPairs(m map[plumbing.Hash]plumbing.Hash) []mapPair {
	pairs := make([]mapPair, 0, len(m))
	for native, compat := range m {
		pairs = append(pairs, mapPair{native: native, compat: compat})
	}
	slices.SortFunc(pairs, func(a, b mapPair) int {
		return bytes.Compare(a.native.Bytes(), b.native.Bytes())
	})
	return pairs
}
