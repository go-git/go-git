// Package packfile implements encoding and decoding of packfiles
// (pack-*.pack), the format Git uses to store and transfer objects.
//
// A packfile holds a sequence of objects, each stored either whole or as a
// delta against another object. [Scanner] and [Parser] read a pack as a
// stream, [Packfile] gives random access to an indexed pack, and [Encoder]
// writes one, optionally choosing deltas through [DeltaSelector]. Only
// version 2 of the format is supported.
//
// The on-disk layout is specified in gitformat-pack(5):
// https://git-scm.com/docs/gitformat-pack
package packfile
