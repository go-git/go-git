package ewah

// A Bitmap represents a read-only EWAH-compressed bitmap.
type Bitmap struct {
	bits  uint32
	words []uint64
	rlw   uint32
}

const (
	RLWRunningBits = 32
	RLWLiteralBits = 64 - 1 - RLWRunningBits

	RLWLargestRunningCount = (1 << RLWRunningBits) - 1
	RLWLargestLiteralCount = (1 << RLWLiteralBits) - 1
)

func GetRunBit(rlw uint64) bool {
	return rlw&1 != 0
}

func GetRunningLen(rlw uint64) uint64 {
	return uint64((rlw >> 1) & RLWLargestRunningCount)
}

func GetLiteralWords(rlw uint64) uint64 {
	return uint64(rlw >> (1 + RLWRunningBits))
}

func (b *Bitmap) Get(pos uint64) bool {
	var i uint64
	for _, word := range b.words {
		runLen := GetRunningLen(word)
		if pos < runLen*64 {
			return GetRunBit(word)
		}
		pos -= runLen * 64

		literals := GetLiteralWords(word)
		if pos < literals*64 {
			litWord := b.words[i+1+pos/64]
			return (litWord & (1 << (pos % 64))) != 0
		}
		pos -= literals * 64

		i++
	}
	return false
}

// ForEach calls fn() for each set bit.
func (b *Bitmap) ForEach(fn func(pos uint64) bool) {
	var pos uint64
	for i := 0; i < len(b.words); i += 2 {
		word := b.words[i]

		if GetRunBit(word) {
			for i := uint64(0); i < GetRunningLen(word)*64; i++ {
				if !fn(pos) {
					return
				}
				pos++
			}
		} else {
			pos += GetRunningLen(word) * 64
		}

		for k := uint64(0); k < GetLiteralWords(word); k++ {
			for j := 0; j < 64; j++ {
				if (b.words[i+1] & (1 << j)) != 0 {
					if !fn(pos) {
						return
					}
				}
				pos++
			}

			pos += 64
		}
	}
}

func (b *Bitmap) NumBits() uint64 {
	var total uint64
	for i := 0; i < len(b.words); i += 2 {
		total += GetRunningLen(b.words[i])*64 + GetLiteralWords(b.words[i])*64
	}

	return total
}

func (b *Bitmap) Bits() uint64 {
	return uint64(b.bits)
}
