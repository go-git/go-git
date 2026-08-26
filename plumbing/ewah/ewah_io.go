package ewah

import (
	"encoding/binary"
	"errors"
	"io"
)

// ErrNilReader is returned by ReadFrom when called with a nil reader.
var ErrNilReader = errors.New("ewah: nil reader")

// readChunkWords bounds how many words a single binary.Read pulls from r. The
// on-disk word count is untrusted, so the payload is read in fixed-size chunks
// rather than allocated up front. A crafted count then fails on a short read
// after growing the slice by at most one chunk, instead of demanding a huge
// allocation from a few header bytes.
const readChunkWords = 1 << 12

// ReadFrom decodes an EWAH-compressed bitmap from r.
func ReadFrom(r io.Reader) (*Bitmap, error) {
	if r == nil {
		return nil, ErrNilReader
	}

	var bits uint32
	if err := binary.Read(r, binary.BigEndian, &bits); err != nil {
		return nil, err
	}

	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}

	words := make([]uint64, 0, min(uint64(count), uint64(readChunkWords)))
	var buf [readChunkWords]uint64
	for remaining := uint64(count); remaining > 0; {
		n := min(remaining, uint64(readChunkWords))
		if err := binary.Read(r, binary.BigEndian, buf[:n]); err != nil {
			return nil, err
		}
		words = append(words, buf[:n]...)
		remaining -= n
	}

	var rlw uint32
	if err := binary.Read(r, binary.BigEndian, &rlw); err != nil {
		return nil, err
	}

	if err := validate(words); err != nil {
		return nil, err
	}

	return &Bitmap{
		words: words,
		rlw:   rlw,
		bits:  bits,
	}, nil
}

// WriteTo encodes the bitmap to w in the EWAH on-disk format, returning the
// number of bytes written.
func (b *Bitmap) WriteTo(w io.Writer) (int64, error) {
	n := int64(0)

	if err := binary.Write(w, binary.BigEndian, b.bits); err != nil {
		return n, err
	}
	n += 4

	if err := binary.Write(w, binary.BigEndian, uint32(len(b.words))); err != nil {
		return n, err
	}
	n += 4

	if err := binary.Write(w, binary.BigEndian, b.words); err != nil {
		return n, err
	}
	n += int64(len(b.words) * 8)

	if err := binary.Write(w, binary.BigEndian, b.rlw); err != nil {
		return n, err
	}
	n += 4

	return n, nil
}
