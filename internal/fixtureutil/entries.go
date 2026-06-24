// Package fixtureutil provides helpers for bridging go-git-fixtures types
// to go-git plumbing types in tests.
package fixtureutil

import (
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
)

// Entries converts a fixture's PackfileEntry (hex-string keys) to a map
// keyed by plumbing.Hash.
func Entries(f *fixtures.Fixture) map[plumbing.Hash]int64 {
	src := f.Entries()
	m := make(map[plumbing.Hash]int64, len(src))
	for h, o := range src {
		m[plumbing.NewHash(h)] = o
	}
	return m
}

// ScannerEntry holds the expected scanner output for a single packfile object,
// with plumbing types resolved from the fixture's plain-typed ScannerEntry.
type ScannerEntry struct {
	Type            plumbing.ObjectType
	Offset          int64
	Size            int64
	Hash            plumbing.Hash
	Reference       plumbing.Hash
	OffsetReference int64
	CRC32           uint32
}

// ScannerEntries converts a fixture's scanner entries to plumbing types.
// Returns nil if the fixture has no scanner entries registered.
func ScannerEntries(f *fixtures.Fixture) []ScannerEntry {
	src := f.ScannerEntries()
	if src == nil {
		return nil
	}
	out := make([]ScannerEntry, len(src))
	for i, e := range src {
		out[i] = ScannerEntry{
			Type:            plumbing.ObjectType(e.Type),
			Offset:          e.Offset,
			Size:            e.Size,
			Hash:            plumbing.NewHash(e.Hash),
			Reference:       plumbing.NewHash(e.Reference),
			OffsetReference: e.OffsetReference,
			CRC32:           e.CRC32,
		}
	}
	return out
}
