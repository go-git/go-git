// Package revfile implements encoding and decoding of packfile reverse
// index (pack-*.rev) files.
//
// A reverse index lists the objects of a pack in the order they are stored
// in it, which makes it cheap to go from a pack offset back to an object.
// Version 1 of the format is supported, for SHA-1 and SHA-256 packs.
//
// This package is thread-safe.
//
// The on-disk layout is specified in gitformat-pack(5):
// https://git-scm.com/docs/gitformat-pack
package revfile
