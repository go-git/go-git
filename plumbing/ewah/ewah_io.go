package ewah

import (
	"encoding/binary"
	"errors"
	"io"
)

// ErrNilReader is returned by ReadFrom when called with a nil reader.
var ErrNilReader = errors.New("ewah: nil reader")

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

	words := make([]uint64, count)
	if err := binary.Read(r, binary.BigEndian, &words); err != nil {
		return nil, err
	}

	var rlw uint32
	if err := binary.Read(r, binary.BigEndian, &rlw); err != nil {
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
