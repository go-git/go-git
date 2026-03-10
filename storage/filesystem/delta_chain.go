package filesystem

import (
	"fmt"

	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

// deltaChainNode describes one delta object in a reconstruction chain.
type deltaChainNode struct {
	packHash   plumbing.Hash // identifies the pack file
	offset     int64         // entry offset in pack file
	dataOffset int64         // zlib payload start offset in pack file
}

// deltaChain describes how to reconstruct one requested delta object.
// It contains the base object location and a list of delta nodes
// ordered from the target object toward the base.
type deltaChain struct {
	baseLoc  deltaChainNode
	baseType plumbing.ObjectType
	baseSize int64
	deltas   []deltaChainNode
}

// buildDeltaChain walks from the starting entry to its base object,
// collecting all intermediate delta nodes. The resulting chain can be
// then resolved by inflating the base and applying deltas in reverse.
func buildDeltaChain(
	startMeta entryMeta,
	startPackHash plumbing.Hash,
	startOffset int64,
	packFiles map[plumbing.Hash]billy.File,
	packIndexes map[plumbing.Hash]*packIndex,
	hashSize int,
) (deltaChain, error) {
	type loc struct {
		packHash plumbing.Hash
		offset   int64
	}

	visited := make(map[loc]struct{})
	var chain deltaChain

	currentPack := startPackHash
	currentOffset := startOffset
	currentMeta := startMeta

	for {
		key := loc{packHash: currentPack, offset: currentOffset}
		if _, ok := visited[key]; ok {
			return deltaChain{}, fmt.Errorf("delta chain: cycle detected at pack %s offset %d", currentPack, currentOffset)
		}
		visited[key] = struct{}{}

		if !currentMeta.typ.IsDelta() {
			chain.baseLoc = deltaChainNode{
				packHash:   currentPack,
				offset:     currentOffset,
				dataOffset: currentMeta.dataOffset,
			}
			chain.baseType = currentMeta.typ
			chain.baseSize = currentMeta.size
			return chain, nil
		}

		chain.deltas = append(chain.deltas, deltaChainNode{
			packHash:   currentPack,
			offset:     currentOffset,
			dataOffset: currentMeta.dataOffset,
		})

		switch currentMeta.typ {
		case plumbing.REFDeltaObject:
			found := false
			for ph, idx := range packIndexes {
				off, err := idx.FindOffset(currentMeta.baseRefHash)
				if err != nil {
					continue
				}
				currentPack = ph
				currentOffset = off
				found = true
				break
			}
			if !found {
				return deltaChain{}, fmt.Errorf("delta chain: cannot find ref-delta base %s", currentMeta.baseRefHash)
			}

		case plumbing.OFSDeltaObject:
			currentOffset = currentMeta.baseOfsOffset
			// OFS_DELTA base is in the same pack file.

		default:
			return deltaChain{}, fmt.Errorf("delta chain: unexpected type %d", currentMeta.typ)
		}

		// Next
		packFile, ok := packFiles[currentPack]
		if !ok {
			return deltaChain{}, fmt.Errorf("delta chain: pack file %s not available", currentPack)
		}

		var err error
		currentMeta, err = readEntryMeta(packFile, currentOffset, hashSize)
		if err != nil {
			return deltaChain{}, fmt.Errorf("delta chain: read entry at pack %s offset %d: %w", currentPack, currentOffset, err)
		}
	}
}

// resolveDeltaChain resolves a delta chain into the final object content.
// It starts from the nearest cached intermediate (or the base object) and
// applies deltas in reverse order toward the target.
func resolveDeltaChain(
	chain deltaChain,
	packFiles map[plumbing.Hash]billy.File,
	cache *deltaBaseCache,
	declaredSize int64,
) (plumbing.ObjectType, []byte, error) {
	typ, content, nextDelta, err := resolveDeltaChainStart(chain, packFiles, cache)
	if err != nil {
		return 0, nil, err
	}

	for i := nextDelta; i >= 0; i-- {
		node := chain.deltas[i]

		packFile, ok := packFiles[node.packHash]
		if !ok {
			return 0, nil, fmt.Errorf("delta resolve: pack file %s not available", node.packHash)
		}

		delta, err := inflateFromPack(packFile, node.dataOffset, -1)
		if err != nil {
			return 0, nil, fmt.Errorf("delta resolve: inflate delta at offset %d: %w", node.dataOffset, err)
		}

		content, err = packfile.PatchDelta(content, delta)
		if err != nil {
			return 0, nil, fmt.Errorf("delta resolve: apply delta at offset %d: %w", node.offset, err)
		}

		cache.put(
			deltaBaseKey{pack: node.packHash, offset: node.offset},
			typ,
			content,
		)
	}

	if declaredSize >= 0 && int64(len(content)) != declaredSize {
		return 0, nil, fmt.Errorf(
			"delta resolve: content size mismatch: got %d want %d",
			len(content), declaredSize,
		)
	}

	return typ, content, nil
}

// resolveDeltaChainStart finds the nearest cached chain node or inflates the
// innermost base object. Returns the starting bytes and the next delta index
// to apply in reverse order.
func resolveDeltaChainStart(
	chain deltaChain,
	packFiles map[plumbing.Hash]billy.File,
	cache *deltaBaseCache,
) (plumbing.ObjectType, []byte, int, error) {
	// Closest to target first, of course.
	for i, node := range chain.deltas {
		typ, content, ok := cache.get(
			deltaBaseKey{pack: node.packHash, offset: node.offset},
		)
		if ok {
			return typ, content, i - 1, nil
		}
	}

	typ, content, ok := cache.get(
		deltaBaseKey{pack: chain.baseLoc.packHash, offset: chain.baseLoc.offset},
	)
	if ok {
		return typ, content, len(chain.deltas) - 1, nil
	}

	packFile, ok := packFiles[chain.baseLoc.packHash]
	if !ok {
		return 0, nil, 0, fmt.Errorf("delta resolve: base pack file %s not available", chain.baseLoc.packHash)
	}

	base, err := inflateFromPack(packFile, chain.baseLoc.dataOffset, chain.baseSize)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("delta resolve: inflate base at offset %d: %w", chain.baseLoc.offset, err)
	}

	cache.put(
		deltaBaseKey{pack: chain.baseLoc.packHash, offset: chain.baseLoc.offset},
		chain.baseType,
		base,
	)

	return chain.baseType, base, len(chain.deltas) - 1, nil
}
