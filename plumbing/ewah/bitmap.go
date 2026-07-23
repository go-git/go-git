// Package ewah provides a read-only decoder for EWAH-compressed bitmaps as
// used by Git, for example in the split index ("link"), untracked cache
// ("UNTR") and file system monitor ("FSMN") index extensions.
//
// An EWAH bitmap is stored as a sequence of 64-bit words. The words are
// grouped into runs, each introduced by a run-length word (RLW):
//
//   - bit 0 holds the value (the "run bit") repeated by the clean words of the
//     run;
//   - the next RLWRunningBits bits hold the running length, i.e. the number of
//     identical "clean" words the run bit stands for;
//   - the remaining bits hold the number of "literal" (verbatim) words that
//     immediately follow the RLW.
//
// Bits within a literal word are stored least-significant-bit first, so bit
// position p maps to 1<<(p%64) of the word at index p/64 within the run.
package ewah

// A Bitmap represents a read-only EWAH-compressed bitmap.
type Bitmap struct {
	// bits is the logical size of the uncompressed bitmap, as stored in the
	// on-disk header. See Bits.
	bits uint32
	// words is the compressed payload: a sequence of run-length words, each
	// followed by its literal words.
	words []uint64
	// rlw is the offset of the last run-length word, as stored on disk. It is
	// retained so the bitmap can be written back out unchanged.
	rlw uint32
}

const (
	// RLWRunningBits is the number of bits used to store the running length
	// in a run-length word.
	RLWRunningBits = 32
	// RLWLiteralBits is the number of bits used to store the literal-word
	// count in a run-length word.
	RLWLiteralBits = 64 - 1 - RLWRunningBits

	// RLWLargestRunningCount is the largest running length representable in a
	// run-length word.
	RLWLargestRunningCount = (1 << RLWRunningBits) - 1
	// RLWLargestLiteralCount is the largest literal-word count representable
	// in a run-length word.
	RLWLargestLiteralCount = (1 << RLWLiteralBits) - 1
)

// RunBit returns whether the run bit in rlw is set.
func RunBit(rlw uint64) bool {
	return rlw&1 != 0
}

// RunningLen extracts rlw's running length, i.e. the number of clean words the
// run represents.
func RunningLen(rlw uint64) uint64 {
	return uint64((rlw >> 1) & RLWLargestRunningCount)
}

// LiteralWords extracts the number of literal words that follow rlw.
func LiteralWords(rlw uint64) uint64 {
	return uint64(rlw >> (1 + RLWRunningBits))
}

// At reports whether the bit at the given position is set. Positions at or
// beyond the bitmap's encoded length report false.
func (b *Bitmap) At(pos uint64) bool {
	var i uint64    // index of the current run-length word
	var seen uint64 // number of bits represented by previous runs

	for i < uint64(len(b.words)) {
		rlw := b.words[i]
		i++

		runBits := RunningLen(rlw) * 64
		if pos-seen < runBits {
			return RunBit(rlw)
		}
		seen += runBits

		literals := LiteralWords(rlw)
		if pos-seen < literals*64 {
			litWord := b.words[i+(pos-seen)/64]
			return litWord&(1<<((pos-seen)%64)) != 0
		}
		seen += literals * 64

		i += literals
	}

	return false
}

// ForEach calls fn for each set bit, in ascending position order.
// The returning bool from fn defines whether iteration should continue: fn
// returning false stops the iteration.
func (b *Bitmap) ForEach(fn func(pos uint64) bool) {
	var pos uint64
	var i uint64

	for i < uint64(len(b.words)) {
		rlw := b.words[i]
		i++

		runBits := RunningLen(rlw) * 64
		if RunBit(rlw) {
			for range runBits {
				if !fn(pos) {
					return
				}
				pos++
			}
		} else {
			pos += runBits
		}

		literals := LiteralWords(rlw)
		for k := range literals {
			word := b.words[i+k]
			for j := range uint64(64) {
				if word&(1<<j) != 0 {
					if !fn(pos) {
						return
					}
				}
				pos++
			}
		}

		i += literals
	}
}

// NumBits returns the number of bits the compressed words actually encode,
// i.e. the sum of every run and literal word rounded up to a 64-bit boundary.
// It is derived from the words themselves and is always a multiple of 64; it
// is therefore greater than or equal to Bits.
func (b *Bitmap) NumBits() uint64 {
	var total uint64
	var i uint64

	for i < uint64(len(b.words)) {
		rlw := b.words[i]
		i++

		literals := LiteralWords(rlw)
		total += RunningLen(rlw)*64 + literals*64
		i += literals
	}

	return total
}

// Bits returns the logical size of the uncompressed bitmap as recorded in the
// on-disk header. Unlike NumBits, which is recomputed from the compressed
// words and rounded up to a whole word, this is the exact bit count the
// producer wrote and may not be a multiple of 64.
func (b *Bitmap) Bits() uint64 {
	return uint64(b.bits)
}
