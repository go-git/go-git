// Package index implements encoding and decoding of the Git index file,
// also known as the staging area or directory cache.
//
// A [Decoder] reads index versions 2, 3 and 4. Besides the entries
// themselves, it decodes the cache tree, resolve undo and end-of-index-entry
// extensions into [Tree], [ResolveUndo] and [EndOfIndexEntry]; other
// optional extensions are skipped, and an unknown mandatory extension is an
// error. An [Encoder] writes the entries in versions 2, 3 and 4, without
// extensions.
//
// The on-disk layout is specified in gitformat-index(5):
// https://git-scm.com/docs/gitformat-index
package index
