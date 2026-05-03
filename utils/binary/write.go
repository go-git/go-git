package binary

import (
	"encoding/binary"
	"errors"
	"io"
)

var errNilWriter = errors.New("nil writer")

// Write writes the binary representation of data into w, using BigEndian order
// https://golang.org/pkg/encoding/binary/#Write
func Write(w io.Writer, data ...any) error {
	if w == nil {
		return errNilWriter
	}

	for _, v := range data {
		if err := binary.Write(w, binary.BigEndian, v); err != nil {
			return err
		}
	}

	return nil
}

// WriteVariableWidthInt writes a variable width encoded int64 to w.
func WriteVariableWidthInt(w io.Writer, n int64) error {
	if w == nil {
		return errNilWriter
	}

	buf := []byte{byte(n & 0x7f)}
	n >>= 7
	for n != 0 {
		n--
		buf = append([]byte{0x80 | (byte(n & 0x7f))}, buf...)
		n >>= 7
	}

	_, err := w.Write(buf)

	return err
}

// WriteUint64 writes the binary representation of a uint64 into w, in BigEndian
// order
func WriteUint64(w io.Writer, value uint64) error {
	if w == nil {
		return errNilWriter
	}

	return binary.Write(w, binary.BigEndian, value)
}

// WriteUint32 writes the binary representation of a uint32 into w, in BigEndian
// order
func WriteUint32(w io.Writer, value uint32) error {
	if w == nil {
		return errNilWriter
	}

	return binary.Write(w, binary.BigEndian, value)
}

// WriteUint16 writes the binary representation of a uint16 into w, in BigEndian
// order
func WriteUint16(w io.Writer, value uint16) error {
	if w == nil {
		return errNilWriter
	}

	return binary.Write(w, binary.BigEndian, value)
}

// Align returns value rounded up to the next multiple of alignment.
func Align(value, alignment uint64) (uint64, error) {
	if alignment == 0 {
		return 0, errors.New("alignment must be greater than zero")
	}
	if rem := value % alignment; rem != 0 {
		return value + (alignment - rem), nil
	}
	return value, nil
}

// WritePadding writes zero bytes until currentLen reaches an alignment boundary.
func WritePadding(w io.Writer, currentLen int, alignment uint64) error {
	if w == nil {
		return errNilWriter
	}
	if currentLen < 0 {
		return errors.New("current length must not be negative")
	}

	aligned, err := Align(uint64(currentLen), alignment)
	if err != nil {
		return err
	}
	padding := int(aligned - uint64(currentLen))
	if padding == 0 {
		return nil
	}

	n, err := w.Write(make([]byte, padding))
	if err != nil {
		return err
	}
	if n != padding {
		return io.ErrShortWrite
	}
	return nil
}
