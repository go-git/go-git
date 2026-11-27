// Package commitgraph implements encoding and decoding of commit-graph
// files.
//
// A commit-graph file records, for the commits it covers, the root tree,
// the parents, the commit time and the generation number, so that history
// can be walked without reading the commit objects. Files can be used on
// their own or as layers of a chain, where each layer extends the ones
// below it. The chunks the format defines are listed in [ChunkType].
//
// The on-disk layout is specified in gitformat-commit-graph(5):
// https://git-scm.com/docs/gitformat-commit-graph
package commitgraph
