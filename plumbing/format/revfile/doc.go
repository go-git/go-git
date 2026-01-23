// Package revfile implements encoding and decoding logic of reverse
// index files (RIDX).
//
// This package is thread-safe for concurrent reads. Close() should not
// be called concurrently with other operations.
//
// RIDX files are named "pack-*.rev" and have the format:
//   - A 4-byte magic number '0x52494458' ('RIDX').
//   - A 4-byte version identifier (= 1).
//   - A 4-byte hash function identifier (= 1 for SHA-1, 2 for SHA-256).
//   - A table of index positions (one per packed object, num_objects in
//     total, each a 4-byte unsigned integer in network order), sorted by
//     their corresponding offsets in the packfile.
//   - A trailer, containing a:
//     checksum of the corresponding packfile, and
//     a checksum of all of the above.
//
// All 4-byte numbers are in network order.
//
// # Usage
//
// Use [NewReaderAtRevIndex] to create a reverse index from a .rev file:
//
//	ri, err := revfile.NewReaderAtRevIndex(revFile, hashSize, count)
//	if err != nil {
//	    return err
//	}
//	defer ri.Close()
//
// Use [ReaderAtRevIndex.LookupIndex] for O(log n) offset-to-index lookups,
// or [ReaderAtRevIndex.All] to iterate over all entries:
//
//	seq, finish := ri.All()
//	for revPos, idxPos := range seq {
//	    // process entries
//	}
//	if err := finish(); err != nil {
//	    return err
//	}
//
// # Checksum Validation
//
// The constructor validates structural integrity but not cryptographic
// checksums. Call [ReaderAtRevIndex.ValidateChecksums] explicitly when
// integrity verification is required.
//
// Refer to:
// https://github.com/git/git/blob/master/Documentation/gitformat-pack.adoc
package revfile
