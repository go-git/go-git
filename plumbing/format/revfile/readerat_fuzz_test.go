package revfile

import (
	"testing"
)

// FuzzReaderAtRevIndex exercises the pack revindex (.rev) ReaderAt parser
// against random input. Seeds include structurally valid .rev files (so the
// fuzzer reaches the iteration and lookup paths), a minimal valid header, and
// an empty input. The verification approach matches the idxfile fuzz targets
// for the original revindex code path: once a reader is constructed, exercise
// every method and assert that none of them panic.
func FuzzReaderAtRevIndex(f *testing.F) {
	// maxFuzzIterations bounds the per-input iteration over the reverse
	// index so a single fuzz input cannot drive an unbounded loop. It
	// mirrors the bounded iteration used by the idxfile fuzz targets (the
	// original .rev/.idx code path). It is declared inside the fuzz target
	// because OSS-Fuzz lifts only the FuzzXxx function body into a standalone
	// file; package-level declarations in this _test.go would not be carried
	// over.
	const maxFuzzIterations = 1 << 20

	// Structurally valid .rev files reach the success path.
	f.Add(buildMinimalRev(3, 20))
	f.Add(buildMinimalRev(0, 20))
	f.Add(buildMinimalRev(3, 32))

	// Minimal rev header: 4-byte magic + version(uint32 BE = 1) +
	// hash-func(uint32 BE = 1, SHA1).
	f.Add([]byte("RIDX\x00\x00\x00\x01\x00\x00\x00\x01"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		for _, hashSize := range []int{20, 32} {
			ri, err := NewReaderAtRevIndex(newMockRevFile(data), hashSize)
			if err != nil {
				// Expected for most fuzz inputs.
				continue
			}

			// Exercise every method; none should panic.
			_ = ri.Count()

			// Bounded sequential iteration over all entries.
			seq, finish := ri.All()
			n := 0
			for range seq {
				if n++; n >= maxFuzzIterations {
					break
				}
			}
			_ = finish()

			// Binary search with a trivial offset getter.
			_, _, _ = ri.LookupIndex(0, func(int) (uint64, error) { return 0, nil })

			// Full-file integrity path (reads and hashes the whole file).
			_ = ri.ValidateChecksums(make([]byte, hashSize))

			_ = ri.Close()
		}
	})
}
