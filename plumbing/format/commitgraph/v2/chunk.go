package v2

import "bytes"

const (
	szChunkSig     = 4 // Length of a chunk signature
	chunkSigOffset = 4 // Offset of each chunk signature in chunkSignatures
)

// chunkSignatures contains the coalesced byte signatures for each chunk type.
// The order of the signatures must match the order of the ChunkType constants.
// (When adding new chunk types you must avoid introducing ambiguity, and you may need to add padding separators to this list or reorder these signatures.)
// (i.e. it would not be possible to add a new chunk type with the signature "IDFO" without some reordering or the addition of separators.)
var chunkSignatures = []byte("OIDFOIDLCDATGDA2GDO2EDGEBIDXBDATBASE\000\000\000\000")

// ChunkType represents the type of a chunk in the commit graph file.
type ChunkType int

const (
	OIDFanoutChunk              ChunkType = iota // "OIDF"
	OIDLookupChunk                               // "OIDL"
	CommitDataChunk                              // "CDAT"
	GenerationDataChunk                          // "GDA2"
	GenerationDataOverflowChunk                  // "GDO2"
	ExtraEdgeListChunk                           // "EDGE"
	BloomFilterIndexChunk                        // "BIDX"
	BloomFilterDataChunk                         // "BDAT"
	BaseGraphsListChunk                          // "BASE"
	ZeroChunk                                    // "\000\000\000\000"
)
const lenChunks = int(ZeroChunk) // ZeroChunk is not a valid chunk type, but it is used to determine the length of the chunk type list.

// Signature returns the byte signature for the chunk type.
func (ct ChunkType) Signature() []byte {
	if ct >= BaseGraphsListChunk || ct < 0 { // not a valid chunk type just return ZeroChunk
		return chunkSignatures[ZeroChunk*chunkSigOffset : ZeroChunk*chunkSigOffset+szChunkSig]
	}

	return chunkSignatures[ct*chunkSigOffset : ct*chunkSigOffset+szChunkSig]
}

// ChunkTypeFromBytes returns the chunk type for the given byte signature.
func ChunkTypeFromBytes(b []byte) (ChunkType, bool) {
	idx := bytes.Index(chunkSignatures, b)
	if idx == -1 || idx%chunkSigOffset != 0 { // not found, or not aligned at chunkSigOffset
		return -1, false
	}
	return ChunkType(idx / chunkSigOffset), true
}
