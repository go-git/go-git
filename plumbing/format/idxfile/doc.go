// Package idxfile implements encoding and decoding of packfile index
// (pack-*.idx) files.
//
// An index maps object ids to their offset and CRC-32 in the accompanying
// packfile. Only version 2 of the format is supported. [MemoryIndex] holds a
// decoded index in memory, [LazyIndex] reads from the file on demand, and
// [Writer] builds an index while a packfile is being parsed.
//
// The on-disk layout is specified in gitformat-pack(5):
// https://git-scm.com/docs/gitformat-pack
package idxfile
